"""Unified CLI for cece benchmarks."""

import argparse
import json
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from pathlib import Path

from . import get_adapter, get_scorer, list_benchmarks
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
    config = _load_config(args.config)
    config["model"] = args.model

    output_dir = args.output_dir or default_output_dir()
    run_id = args.run_id or default_run_id(args.model)
    store = ResultStore(output_dir, args.benchmark, run_id)

    print(f"Benchmark: {args.benchmark}")
    print(f"Model: {args.model}")
    print(f"Dataset: {args.dataset}  Split: {args.split}")
    print(f"Output: {output_dir}/{args.benchmark}/{run_id}")
    print(f"Workers: {args.max_workers}  Timeout: {args.timeout}s")

    instances = adapter.load_instances(args.dataset, args.split, args.slice)
    print(f"Instances: {len(instances)}")

    if args.max_workers <= 1:
        for inst in instances:
            _run_one(adapter, inst, args.cece_bin, config, args.timeout, store)
    else:
        with ThreadPoolExecutor(max_workers=args.max_workers) as executor:
            futures = {
                executor.submit(_run_one, adapter, inst, args.cece_bin, config, args.timeout, store): adapter.instance_id(inst)
                for inst in instances
            }
            for future in as_completed(futures):
                future.result()

    records = store.load_runs()
    _write_predictions(store, records, adapter, args.benchmark)
    _print_summary(records)
    store.close()


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

    print(f"Scoring {args.benchmark} from {result_dir}")
    report = scorer.score(result_dir, args.dataset or "", args.split or "")

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

    print(f"[run] {inst_id}")

    sandbox = None
    try:
        sandbox = adapter.setup_sandbox(inst, cece_bin, config)
        driver = CeceDriver.start(sandbox.exec_cmd, env=sandbox.env)
        prompt = adapter.build_prompt(inst)
        result = driver.run_until_done(prompt, timeout=timeout)
        driver.close()

        artifact = {}
        if result.exit_status == "completed":
            try:
                artifact = adapter.collect_artifact(sandbox, inst)
            except Exception as e:
                artifact = {"collection_error": str(e)}

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

        status = result.exit_status
        if status == "completed":
            art_size = sum(len(str(v)) for v in artifact.values())
            print(f"  [done] {inst_id} — {status} | artifact:{art_size}b")
        else:
            print(f"  [fail] {inst_id} — {status}")

    except Exception as e:
        print(f"  [error] {inst_id} — {e}")
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
    print(f"\nDone: {done} | Failed: {failed} | Skipped: {skipped} | Total: {len(records)}")


def _load_config(config_path):
    if not os.path.exists(config_path):
        config_path = os.path.expanduser(config_path)
    with open(config_path) as f:
        return json.load(f)


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
    p_run.add_argument("--config", default="~/.cece/settings.json")
    p_run.add_argument("--cece-bin", required=True, help="Path to cece linux/amd64 binary")
    p_run.add_argument("--max-workers", type=int, default=4)
    p_run.add_argument("--timeout", type=int, default=600)
    p_run.add_argument("--slice", help="Slice instances (e.g. ':10' for first 10)")
    p_run.add_argument("--output-dir", help="Output directory")
    p_run.add_argument("--run-id", help="Run identifier")

    # score
    p_score = sub.add_parser("score", help="Score a benchmark run")
    p_score.add_argument("benchmark", choices=BENCHMARK_LIST)
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