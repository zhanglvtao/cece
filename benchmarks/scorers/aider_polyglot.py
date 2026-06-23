"""Aider polyglot scorer — runs benchmark.py --stats."""

import json
import os
import subprocess
from pathlib import Path

from . import ScoreReport, Scorer


class AiderPolyglotScorer(Scorer):
    name = "aider-polyglot"

    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        from ..setup import get_setup_info

        info = get_setup_info("aider-polyglot")
        if not info:
            return ScoreReport(details=[{"error": "aider-polyglot not set up. Run: python -m benchmarks setup --benchmark aider-polyglot"}])

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
                art = rec.get("artifact", {})
                if art.get("passed", False):
                    passed += 1
                details.append({
                    "instance_id": rec.get("instance_id", ""),
                    "passed": art.get("passed", False),
                    "exit_status": rec.get("exit_status", "unknown"),
                })

        pass_rate = (passed / total * 100) if total > 0 else 0.0
        return ScoreReport(total=total, resolved=passed, pass_rate=pass_rate, details=details)