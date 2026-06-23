"""Multi-SWE-bench scorer — calls official multi_swe_bench evaluation harness."""

import json
import os
import subprocess
from pathlib import Path

from . import ScoreReport, Scorer


class MSWEBenchScorer(Scorer):
    name = "mswe"

    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        from ..setup import get_setup_info

        info = get_setup_info("mswe")
        if not info:
            return ScoreReport(details=[{"error": "mswe not set up. Run: python -m benchmarks setup --benchmark mswe"}])

        python = info["python"]
        repo = info["repo"]

        # Build patch file from runs.jsonl
        runs_file = os.path.join(result_dir, "runs.jsonl")
        if not os.path.exists(runs_file):
            return ScoreReport(details=[{"error": f"runs file not found: {runs_file}"}])

        patch_file = os.path.join(result_dir, "patches.jsonl")
        with open(runs_file) as f_in, open(patch_file, "w") as f_out:
            for line in f_in:
                line = line.strip()
                if not line:
                    continue
                rec = json.loads(line)
                art = rec.get("artifact", {})
                fix_patch = art.get("fix_patch", art.get("patch", ""))
                if fix_patch:
                    f_out.write(json.dumps({
                        "org": art.get("org", ""),
                        "repo": art.get("repo", ""),
                        "number": art.get("number", ""),
                        "fix_patch": fix_patch,
                    }) + "\n")

        eval_dir = os.path.join(result_dir, "eval")
        os.makedirs(eval_dir, exist_ok=True)

        config = {
            "mode": "evaluation",
            "workdir": eval_dir,
            "patch_files": [patch_file],
            "dataset_files": [dataset],
            "force_build": False,
            "output_dir": eval_dir,
            "log_dir": os.path.join(eval_dir, "logs"),
            "max_workers": 4,
            "log_level": "INFO",
        }
        config_file = os.path.join(eval_dir, "config.json")
        with open(config_file, "w") as f:
            json.dump(config, f, indent=2)

        try:
            result = subprocess.run(
                [python, "-m", "multi_swe_bench.harness.run_evaluation", "--config", config_file],
                capture_output=True, text=True, timeout=600,
                cwd=repo,
            )
            report = self._parse_output(result.stdout, result.stderr, eval_dir)
            return report
        except subprocess.TimeoutExpired:
            return ScoreReport(details=[{"error": "scoring timed out"}])
        except Exception as e:
            return ScoreReport(details=[{"error": str(e)}])

    def _parse_output(self, stdout: str, stderr: str, eval_dir: str) -> ScoreReport:
        report = ScoreReport()
        # Check for final_report.json
        report_path = os.path.join(eval_dir, "final_report.json")
        if os.path.exists(report_path):
            with open(report_path) as f:
                data = json.load(f)
            report.total = data.get("total", 0)
            report.resolved = data.get("resolved_instances", 0)
            if report.total > 0:
                report.pass_rate = report.resolved / report.total * 100
            report.details.append(data)
        else:
            for line in (stdout + stderr).split("\n"):
                if line.strip():
                    report.details.append({"raw": line.strip()})
        return report