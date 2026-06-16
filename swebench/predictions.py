"""JSONL prediction file I/O."""

import json
import os
from pathlib import Path
from typing import Optional


def load_predictions(path: Path) -> dict[str, dict]:
    """Load predictions.jsonl into {instance_id: prediction_row}."""
    if not path.exists():
        return {}
    preds = {}
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            row = json.loads(line)
            preds[row["instance_id"]] = row
    return preds


def save_prediction(
    path: Path,
    instance_id: str,
    model_name: str,
    model_patch: str,
) -> None:
    """Write or update a single prediction row in JSONL file."""
    preds = load_predictions(path)
    preds[instance_id] = {
        "instance_id": instance_id,
        "model_name_or_path": model_name,
        "model_patch": model_patch,
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w") as f:
        for row in preds.values():
            f.write(json.dumps(row) + "\n")


def save_result(
    path: Path,
    instance_id: str,
    model_name: str,
    model_patch: str,
    stats: Optional[dict] = None,
) -> None:
    """Write detailed result (predictions + stats)."""
    save_prediction(path, instance_id, model_name, model_patch)
    if stats:
        stats_path = path.with_suffix(".stats.jsonl")
        stats["instance_id"] = instance_id
        stats["patch"] = model_patch[:200] + "..." if len(model_patch) > 200 else model_patch
        with open(stats_path, "a") as f:
            f.write(json.dumps(stats) + "\n")