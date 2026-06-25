"""Unified CLI for cece benchmarks."""

import argparse
import json
import os
import platform
import subprocess
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from . import get_adapter, get_scorer, list_benchmarks, list_scorers
from .driver import CeceDriver
from .result import ResultStore, RunRecord, default_output_dir, default_run_id

BENCHMARK_LIST = list_benchmarks()


def cmd_setup(args):
    from .setup import setup_benchmark, setup_all

    if args.all:
        setup_all()
    elif args.benchmark:
        setup_benchmark(args.benchmark)
    else:
        print("Usage: python -m benchmarks setup --benchmark <name> | --all")


def cmd_list(args):
    print("Available benchmarks:")
    for name in BENCHMARK_LIST:
        adapter = get_adapter(name)
        print(f"  {name:20s}  docker={adapter.requires_docker}  timeout={adapter.default_timeout}s")


def cmd_run(args):
    adapter = get_adapter(args.benchmark)
    config = _load_config_for_benchmark(args.benchmark, args.config)
    config["model"] = args.model

    output_dir = args.output_dir or default_output_dir()
    run_id = args.run_id or default_run_id(args.model)
    store = ResultStore(output_dir, args.benchmark, run_id)

    print(f"Benchmark: {args.benchmark}")
    print(f"Model: {args.model}")
    print(f"Dataset: {args.dataset}  Split: {args.split}")
    print(f"Output: {output_dir}/{args.benchmark}/{run_id}")
    print(f"Concurrency: {args.concurrency}  Timeout: {args.timeout}s")

    instances = adapter.load_instances(args.dataset, args.split, args.slice)
    print(f"Instances: {len(instances)}")

    cece_bin = args.cece_bin or _default_cece_bin()
    if instances:
        cece_bin = _ensure_cece_binary(cece_bin)
        _ensure_benchmark_runtime(adapter, instances, cece_bin)

    if args.concurrency <= 1:
        for inst in instances:
            _run_one(adapter, inst, cece_bin, config, args.timeout, store)
    else:
        with ThreadPoolExecutor(max_workers=args.concurrency) as executor:
            futures = {
                executor.submit(_run_one, adapter, inst, cece_bin, config, args.timeout, store): adapter.instance_id(inst)
                for inst in instances
            }
            for future in as_completed(futures):
                future.result()

    records = store.load_runs()
    _write_predictions(store, records, adapter, args.benchmark)
    _print_summary(records)
    store.close()

    # Notify when benchmark run completes
    try:
        resolved = sum(1 for r in records if r.artifact.get("score", {}).get("resolved", False))
        total = len(records)
        msg = f"Benchmark {args.benchmark} done: {resolved}/{total} resolved"
        subprocess.run([
            "osascript", "-e",
            f'display notification "{msg}" with title "cece benchmark"'
        ], check=False, capture_output=True)
    except Exception:
        pass


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _ensure_cece_binary(cece_bin: str) -> str:
    path = Path(cece_bin).expanduser()
    if not path.is_absolute():
        path = _repo_root() / path

    arch = "amd64" if "amd64" in path.name else "arm64" if "arm64" in path.name else _host_goarch()
    make_target = "build-linux" if arch == "amd64" else "build-linux-arm64"
    if path.exists():
        print(f"[prepare] rebuilding cece binary for this benchmark run: {path}", flush=True)
    else:
        print(f"[prepare] cece binary missing: {path}", flush=True)
    print(f"[prepare] building cece via `make {make_target}`", flush=True)
    subprocess.run(["make", make_target], cwd=str(_repo_root()), check=True)

    if not path.exists():
        raise FileNotFoundError(f"cece binary was not produced: {path}")
    print(f"[prepare] cece binary ready: {path}", flush=True)
    return str(path)


def _host_goarch() -> str:
    machine = platform.machine().lower()
    if machine in ("x86_64", "amd64"):
        return "amd64"
    if machine in ("arm64", "aarch64"):
        return "arm64"
    return machine


def _default_cece_bin() -> str:
    return f"./bin/cece-linux-{_host_goarch()}"


def _ensure_benchmark_runtime(adapter, instances: list[dict], cece_bin: str) -> None:
    if adapter.name != "swebench":
        return
    from .build_images import ensure_env_images_for_instances, platform_for_cece_binary

    print("[prepare] checking SWE-bench env images for selected instances", flush=True)
    ensure_env_images_for_instances(instances, platform_name=platform_for_cece_binary(cece_bin))


def cmd_score(args):
    scorer = get_scorer(args.benchmark)

    result_dir = args.result_dir
    if not result_dir:
        # Try to find latest
        base = Path(default_output_dir()) / args.benchmark
        if base.exists():
            dirs = sorted(base.iterdir(), reverse=True)
            if dirs:
                result_dir = str(dirs[0])

    if not result_dir:
        print("No result directory found. Use --result-dir.")
        return

    dataset = getattr(args, 'dataset', '') or ''
    split = getattr(args, 'split', '') or ''

    print(f"Scoring {args.benchmark} from {result_dir}")
    report = scorer.score(result_dir, dataset, split)

    print(f"  Total: {report.total}")
    print(f"  Resolved: {report.resolved}")
    print(f"  Pass rate: {report.pass_rate:.1f}%")

    if report.details:
        report_path = os.path.join(result_dir, "score_report.json")
        with open(report_path, "w") as f:
            json.dump({
                "total": report.total,
                "resolved": report.resolved,
                "pass_rate": report.pass_rate,
                "details": report.details,
            }, f, indent=2)
        print(f"  Report saved: {report_path}")


def _run_one(adapter, inst, cece_bin, config, timeout, store):
    inst_id = adapter.instance_id(inst)
    started_at = datetime.now(timezone.utc).isoformat()
    t0 = time.monotonic()

    def log(message: str) -> None:
        elapsed = time.monotonic() - t0
        print(f"  [{elapsed:7.1f}s] {message}", flush=True)

    print(f"[run] {inst_id}", flush=True)

    sandbox = None
    try:
        log("[setup] creating sandbox")
        sandbox = adapter.setup_sandbox(inst, cece_bin, config)
        log("[setup] sandbox ready")
        log("[engine] starting cece engine")
        driver = CeceDriver.start(sandbox.exec_cmd, env=sandbox.env)
        # Set up early-done polling for swebench
        poll_thread = None
        if adapter.name == "swebench":
            container_name = sandbox.extra.get("container_name", "")
            if container_name:
                def _poll_done():
                    while not driver.early_done_event.is_set():
                        try:
                            r = subprocess.run(
                                ["docker", "exec", container_name, "test", "-f", "/testbed/.cece/done"],
                                capture_output=True, timeout=5
                            )
                            if r.returncode == 0:
                                driver.early_done_event.set()
                                break
                        except Exception:
                            pass
                        time.sleep(5)
                poll_thread = threading.Thread(target=_poll_done, daemon=True)
                poll_thread.start()
        prompt = adapter.build_prompt(inst)
        log(f"[engine] sending prompt ({len(prompt)} chars)")
        result = driver.run_until_done(prompt, timeout=timeout)
        driver.close()
        if poll_thread is not None:
            poll_thread.join(timeout=2)
        log(f"[engine] done, exit_status={result.exit_status}")

        artifact = {}
        score_result = {}
        if result.exit_status == "completed":
            try:
                log("[diff] collecting source diff")
                artifact = adapter.collect_artifact(sandbox, inst)
                patch = artifact.get("patch", "")
                log(f"[diff] collected patch ({len(patch.encode())} bytes)")
            except Exception as e:
                artifact = {"collection_error": str(e)}
                log(f"[diff] collection failed: {e}")

            if adapter.name == "swebench":
                # In-place scoring: same container, apply patch + run tests
                try:
                    from .scorers.swebench import score_in_place
                    container_name = sandbox.extra.get("container_name", "")
                    instance_data = sandbox.extra.get("instance", inst)
                    patch = artifact.get("patch", "")
                    if container_name and patch:
                        log("[score] starting in-place apply + test scoring")
                        score_result = score_in_place(container_name, patch, instance_data, timeout=timeout, log=log)
                        artifact["score"] = score_result
                    elif not patch:
                        score_result = {"status": "no_patch", "resolved": False}
                        artifact["score"] = score_result
                        log("[score] skipped because patch is empty")
                    else:
                        score_result = {"status": "missing_container", "resolved": False}
                        artifact["score"] = score_result
                        log("[score] skipped because container metadata is missing")
                except Exception as e:
                    score_result = {"status": "score_error", "error": str(e)}
                    artifact["score"] = score_result
                    log(f"[score] failed: {e}")

        record = RunRecord(
            benchmark=adapter.name,
            instance_id=inst_id,
            model=config.get("model", "unknown"),
            exit_status=result.exit_status,
            artifact=artifact,
            stats=result.stats,
            transcript=result.transcript,
            started_at=started_at,
            duration_s=time.monotonic() - t0,
            error=result.error,
        )
        store.save_run(record)
        log("[result] saved run record and transcript")

        status = result.exit_status
        score_status = score_result.get("status", "")
        resolved = score_result.get("resolved", False)
        if status == "completed":
            print(f"  [{'PASS' if resolved else 'FAIL'}] {inst_id} — {score_status or status}", flush=True)
        else:
            print(f"  [FAIL] {inst_id} — {status}", flush=True)

    except Exception as e:
        import traceback
        print(f"  [error] {inst_id} — {e}", flush=True)
        traceback.print_exc()
        record = RunRecord(
            benchmark=adapter.name,
            instance_id=inst_id,
            model=config.get("model", "unknown"),
            exit_status="error",
            error=str(e),
            started_at=started_at,
            duration_s=time.monotonic() - t0,
        )
        store.save_run(record)
    finally:
        if sandbox:
            try:
                print(f"  [cleanup] removing sandbox", flush=True)
                adapter.teardown_sandbox(sandbox)
            except Exception:
                pass


def _write_predictions(store, records, adapter, benchmark):
    """Export predictions in benchmark-specific format."""
    result_dir = str(store._dir)
    # SWE-bench / MSWE format
    predictions = []
    for rec in records:
        if rec.exit_status != "completed":
            continue
        patch = rec.artifact.get("patch", "")
        if not patch:
            continue
        predictions.append({
            "instance_id": rec.instance_id,
            "model_name_or_path": f"cece/{rec.model}",
            "model_patch": patch,
        })
    if predictions:
        pred_path = os.path.join(result_dir, "predictions.jsonl")
        with open(pred_path, "w") as f:
            for p in predictions:
                f.write(json.dumps(p) + "\n")


def _print_summary(records):
    done = sum(1 for r in records if r.exit_status == "completed")
    failed = sum(1 for r in records if r.exit_status not in ("completed", "skipped"))
    skipped = sum(1 for r in records if r.exit_status == "skipped")
    resolved = sum(1 for r in records if r.artifact.get("score", {}).get("resolved", False))
    print(f"\nDone: {done} | Failed: {failed} | Skipped: {skipped} | Resolved: {resolved} | Total: {len(records)}")


def _load_config_for_benchmark(benchmark: str, config_path: Optional[str] = None) -> dict:
    if config_path:
        if not os.path.exists(config_path):
            config_path = os.path.expanduser(config_path)
        with open(config_path) as f:
            return json.load(f)

    # 1. Try user's real settings first (~/.cece/settings.json)
    user_settings = os.path.expanduser("~/.cece/settings.json")
    if os.path.exists(user_settings):
        with open(user_settings) as f:
            config = json.load(f)
    else:
        # 2. Fallback to benchmark default
        import benchmarks
        pkg_dir = os.path.dirname(os.path.abspath(benchmarks.__file__))
        default_path = os.path.join(pkg_dir, "configs", f"{benchmark}.json")
        if os.path.exists(default_path):
            with open(default_path) as f:
                config = json.load(f)
        else:
            raise FileNotFoundError(
                f"No config found for {benchmark}. "
                f"Expected {default_path} or ~/.cece/settings.json or pass --config."
            )

    # Override benchmark-specific runtime settings
    import benchmarks as pkg
    pkg_dir = os.path.dirname(os.path.abspath(pkg.__file__))
    bench_cfg_path = os.path.join(pkg_dir, "configs", f"{benchmark}.json")
    if os.path.exists(bench_cfg_path):
        with open(bench_cfg_path) as f:
            bench_cfg = json.load(f)
        for key in ("defaultMode", "yolo", "tool_result"):
            if key in bench_cfg:
                config[key] = bench_cfg[key]

    return config


def main():
    parser = argparse.ArgumentParser(description="cece benchmarks — unified evaluation suite")
    sub = parser.add_subparsers(dest="command")

    # setup
    p_setup = sub.add_parser("setup", help="Install benchmark dependencies")
    p_setup.add_argument("--benchmark", choices=BENCHMARK_LIST)
    p_setup.add_argument("--all", action="store_true")

    # list
    sub.add_parser("list", help="List available benchmarks")

    # run
    p_run = sub.add_parser("run", help="Run a benchmark")
    p_run.add_argument("benchmark", choices=BENCHMARK_LIST)
    p_run.add_argument("--dataset", default="princeton-nlp/SWE-bench_Lite")
    p_run.add_argument("--split", default="test")
    p_run.add_argument("--model", default="deepseek-v4-pro")
    p_run.add_argument("--config", default=None, help="Path to settings.json (default: benchmarks/configs/<benchmark>.json)")
    p_run.add_argument("--cece-bin", default=None, help="Path to cece Linux binary (default: ./bin/cece-linux-<host-arch>, auto-built if missing)")
    p_run.add_argument("--concurrency", type=int, default=4, help="Number of benchmark cases to run in parallel")
    p_run.add_argument("--timeout", type=int, default=600)
    p_run.add_argument("--slice", help="Slice instances (e.g. ':10' for first 10)")
    p_run.add_argument("--output-dir", help="Output directory")
    p_run.add_argument("--run-id", help="Run identifier")

    # score
    p_score = sub.add_parser("score", help="Score a benchmark run")
    p_score.add_argument("benchmark", choices=list_scorers())
    p_score.add_argument("--result-dir", help="Path to result directory")
    p_score.add_argument("--dataset", default="")
    p_score.add_argument("--split", default="")

    args = parser.parse_args()

    if args.command == "setup":
        cmd_setup(args)
    elif args.command == "list":
        cmd_list(args)
    elif args.command == "run":
        cmd_run(args)
    elif args.command == "score":
        cmd_score(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
