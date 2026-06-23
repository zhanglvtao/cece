"""Terminal-bench scorer — uses tb CLI."""

import json
import os
import subprocess
from pathlib import Path

from . import ScoreReport, Scorer


class TerminalBenchScorer(Scorer):
    name = "terminal-bench"

    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        from ..setup import get_setup_info

        info = get_setup_info("terminal-bench")
        if not info:
            return ScoreReport(details=[{"error": "terminal-bench not set up. Run: python -m benchmarks setup --benchmark terminal-bench"}])

        python = info["python"]

        runs_file = os.path.join(result_dir, "runs.jsonl")
        if not os.path.exists(runs_file):
            return ScoreReport(details=[{"error": f"runs file not found: {runs_file}"}])

        total = 0
        passed = 0
        details = []

        with open(runs_file) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                rec = json.loads(line)
                total += 1
                exit_status = rec.get("exit_status", "unknown")
                if exit_status == "completed":
                    passed += 1
                details.append({
                    "instance_id": rec.get("instance_id", ""),
                    "exit_status": exit_status,
                })

        pass_rate = (passed / total * 100) if total > 0 else 0.0
        return ScoreReport(total=total, resolved=passed, pass_rate=pass_rate, details=details)