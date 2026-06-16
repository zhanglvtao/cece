"""SWE-bench batch runner for cece.

Usage:
    python -m swebench.runner \\
        --dataset princeton-nlp/SWE-bench_Lite \\
        --split test \\
        --model deepseek-v4-pro \\
        --config ~/.cece/settings.json \\
        --cece-bin ./bin/cece \\
        --max-workers 4 \\
        --timeout 600 \\
        --predictions-path ./predictions.jsonl
"""

import argparse
import json
import os
import subprocess
import sys
import time
import traceback
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any, Optional

from swebench.docker import DockerInstance
from swebench.driver import CeceDriver
from swebench.predictions import save_result, load_predictions


def load_dataset(dataset_name: str, split: str) -> list[dict[str, Any]]:
    """Load SWE-bench dataset from HuggingFace."""
    try:
        from datasets import load_dataset
    except ImportError:
        sys.exit("pip install datasets")

    ds = load_dataset(dataset_name, split=split)
    return list(ds)


def run_instance(
    instance: dict[str, Any],
    cece_bin: str,
    model: str,
    config_path: str,
    timeout: int,
    predictions_path: Path,
) -> dict[str, Any]:
    """Run a single SWE-bench instance."""
    instance_id = instance["instance_id"]
    base_commit = instance["base_commit"]
    problem_statement = instance["problem_statement"]

    # Check if already done
    existing = load_predictions(predictions_path)
    if instance_id in existing:
        print(f"[skip] {instance_id} — already in predictions")
        return {"instance_id": instance_id, "status": "skipped"}

    print(f"[run] {instance_id}")

    docker = DockerInstance(
        instance_id=instance_id,
        cece_bin=cece_bin,
        model=model,
        config_path=config_path,
    )

    try:
        docker.pull_image()
        docker.start(base_commit=base_commit, problem_statement=problem_statement)
    except Exception as e:
        print(f"[error] {instance_id} — docker setup failed: {e}")
        return {"instance_id": instance_id, "status": "docker_error", "error": str(e)}

    patch = ""
    stats = None
    exit_status = "error"

    try:
        proc = subprocess.Popen(
            ["docker", "exec", "-i", docker.container_name, "/usr/local/bin/cece", "engine", "--project-dir", "/testbed"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        driver = CeceDriver(proc)

        result = driver.run_until_done(
            input_text=(
                f"Please fix the following GitHub issue in the /testbed codebase.\n\n"
                f"<issue>\n{problem_statement}\n</issue>"
            ),
            timeout=timeout,
        )

        exit_status = result["exit_status"]
        stats = result["stats"]
        patch = docker.get_patch()
        driver.close()

    except subprocess.TimeoutExpired:
        exit_status = "timeout"
        print(f"[timeout] {instance_id} — {timeout}s exceeded")
    except Exception as e:
        exit_status = "error"
        print(f"[error] {instance_id} — {e}")
        traceback.print_exc()
    finally:
        try:
            docker.stop()
        except Exception:
            pass

    if patch.strip():
        save_result(predictions_path, instance_id, f"cece/{model}", patch, stats=stats)
        print(f"[done] {instance_id} — {exit_status} | patch:{len(patch)}b | {_format_stats(stats)}")
    else:
        print(f"[fail] {instance_id} — {exit_status} | empty patch")

    return {"instance_id": instance_id, "status": exit_status, "patch_size": len(patch), "stats": stats}


def _format_stats(stats: Optional[dict]) -> str:
    if not stats:
        return "no stats"
    parts = []
    parts.append(f"turns:{stats.get('turn_count', 0)}")
    parts.append(f"api:{stats.get('api_calls', 0)}")
    parts.append(f"in:{_k(stats.get('total_input_tokens', 0))}")
    parts.append(f"out:{_k(stats.get('total_output_tokens', 0))}")
    cache_read = stats.get("cache_read_tokens", 0)
    cache_creation = stats.get("cache_creation_tokens", 0)
    if cache_read or cache_creation:
        pct = stats.get("last_input_tokens", 0)
        if pct > 0:
            hit = int(cache_read / (cache_read + cache_creation + pct) * 100) if (cache_read + cache_creation + pct) > 0 else 0
            parts.append(f"cache:{_k(cache_read)}({hit}%)")
    tools = stats.get("tool_success_counts", {})
    if tools:
        tool_str = " ".join(f"{k}:{v}" for k, v in sorted(tools.items()))
        parts.append(tool_str)
    return " | ".join(parts)


def _k(n: int) -> str:
    if n >= 1000:
        return f"{n // 1000}K"
    return str(n)


def main():
    parser = argparse.ArgumentParser(description="cece SWE-bench runner")
    parser.add_argument("--dataset", default="princeton-nlp/SWE-bench_Lite", help="HuggingFace dataset name")
    parser.add_argument("--split", default="test", help="Dataset split")
    parser.add_argument("--instance-id", help="Run a single instance (overrides --dataset)")
    parser.add_argument("--model", default="deepseek-v4-pro", help="Model name")
    parser.add_argument("--config", default="~/.cece/settings.json", help="Path to cece settings.json")
    parser.add_argument("--cece-bin", required=True, help="Path to cece binary (linux/amd64)")
    parser.add_argument("--max-workers", type=int, default=4, help="Max parallel workers")
    parser.add_argument("--timeout", type=int, default=600, help="Timeout per instance (seconds)")
    parser.add_argument("--predictions-path", default="./predictions.jsonl", help="Output predictions file")
    parser.add_argument("--slice", help="Slice instances (e.g. ':10' for first 10)")
    args = parser.parse_args()

    predictions_path = Path(args.predictions_path).resolve()

    if args.instance_id:
        instances = [{
            "instance_id": args.instance_id,
            "base_commit": "HEAD",
            "problem_statement": "Please fix the issue described.",
        }]
    else:
        instances = load_dataset(args.dataset, args.split)
        if args.slice:
            instances = eval(f"instances[{args.slice}]")

    print(f"Running {len(instances)} instances, workers={args.max_workers}, timeout={args.timeout}s")
    print(f"Model: {args.model}  Config: {args.config}")
    print(f"Predictions → {predictions_path}")

    if args.max_workers <= 1:
        results = []
        for inst in instances:
            results.append(run_instance(
                inst, args.cece_bin, args.model, args.config,
                args.timeout, predictions_path,
            ))
    else:
        with ThreadPoolExecutor(max_workers=args.max_workers) as executor:
            futures = {
                executor.submit(
                    run_instance,
                    inst, args.cece_bin, args.model, args.config,
                    args.timeout, predictions_path,
                ): inst["instance_id"]
                for inst in instances
            }
            results = []
            for future in as_completed(futures):
                results.append(future.result())

    done = sum(1 for r in results if r["status"] == "completed")
    skipped = sum(1 for r in results if r["status"] == "skipped")
    failed = len(results) - done - skipped
    print(f"\nDone: {done} | Skipped: {skipped} | Failed: {failed} | Total: {len(results)}")
    print(f"Predictions → {predictions_path}")


if __name__ == "__main__":
    main()