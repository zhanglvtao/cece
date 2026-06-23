"""SWE-bench scorer — calls official swe-bench evaluation harness."""

import json
import os
import subprocess
from pathlib import Path

from . import ScoreReport, Scorer


class SWEBenchScorer(Scorer):
    name = "swebench"

    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        from ..setup import get_setup_info

        info = get_setup_info("swebench")
        if not info:
            return ScoreReport(details=[{"error": "swebench not set up. Run: python -m benchmarks setup --benchmark swebench"}])

        python = info["python"]
        repo = info["repo"]

        predictions_file = os.path.join(result_dir, "predictions.jsonl")
        if not os.path.exists(predictions_file):
            return ScoreReport(details=[{"error": f"predictions file not found: {predictions_file}"}])

        # Build evaluation config
        eval_dir = os.path.join(result_dir, "eval")
        os.makedirs(eval_dir, exist_ok=True)

        try:
            result = subprocess.run(
                [
                    python, "-m", "swebench.harness.run_evaluation",
                    "--dataset_name", dataset,
                    "--split", split,
                    "--predictions_path", predictions_file,
                    "--max_workers", "4",
                    "--run_id", "cece-eval",
                    "--cache_dir", eval_dir,
                ],
                capture_output=True, text=True, timeout=600,
                cwd=repo,
            )
            return self._parse_output(result.stdout, result.stderr)
        except subprocess.TimeoutExpired:
            return ScoreReport(details=[{"error": "scoring timed out"}])
        except Exception as e:
            return ScoreReport(details=[{"error": str(e)}])

    def _parse_output(self, stdout: str, stderr: str) -> ScoreReport:
        # Try to find a report.json produced by the harness
        report = ScoreReport()
        for line in (stdout + stderr).split("\n"):
            if "resolved" in line.lower() or "pass" in line.lower():
                report.details.append({"raw": line.strip()})
        return report