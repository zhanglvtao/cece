"""Spider2 scorer — compares SQL answers against gold."""

import json
import os
import subprocess
from pathlib import Path

from . import ScoreReport, Scorer


class Spider2Scorer(Scorer):
    name = "spider2"

    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        from ..setup import get_setup_info

        info = get_setup_info("spider2")
        if not info:
            return ScoreReport(details=[{"error": "spider2 not set up. Run: python -m benchmarks setup --benchmark spider2"}])

        runs_file = os.path.join(result_dir, "runs.jsonl")
        if not os.path.exists(runs_file):
            return ScoreReport(details=[{"error": f"runs file not found: {runs_file}"}])

        total = 0
        resolved = 0
        details = []

        with open(runs_file) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                rec = json.loads(line)
                total += 1
                art = rec.get("artifact", {})
                answer = art.get("answer", "")
                gold = art.get("gold", "")
                # Simple string match; in practice use execution match
                correct = answer.strip().lower() == gold.strip().lower() if answer and gold else False
                if correct:
                    resolved += 1
                details.append({
                    "instance_id": rec.get("instance_id", ""),
                    "correct": correct,
                    "exit_status": rec.get("exit_status", "unknown"),
                })

        pass_rate = (resolved / total * 100) if total > 0 else 0.0
        return ScoreReport(total=total, resolved=resolved, pass_rate=pass_rate, details=details)