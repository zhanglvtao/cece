"""Unified result store for benchmark runs."""

import json
import os
import threading
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional


@dataclass
class RunRecord:
    benchmark: str
    instance_id: str
    model: str
    exit_status: str
    artifact: dict = field(default_factory=dict)
    stats: Optional[dict] = None
    transcript: list[dict] = field(default_factory=list)
    started_at: str = ""
    duration_s: float = 0.0
    error: Optional[str] = None


class ResultStore:
    def __init__(self, output_dir: str, benchmark: str, run_id: str):
        self._dir = Path(output_dir) / benchmark / run_id
        self._dir.mkdir(parents=True, exist_ok=True)
        (self._dir / "logs").mkdir(exist_ok=True)
        self._lock = threading.Lock()

    def save_run(self, record: RunRecord) -> None:
        with self._lock:
            self._append_jsonl(self._dir / "runs.jsonl", record)
            self._write_log(record)

    def _append_jsonl(self, path: Path, record: RunRecord) -> None:
        d = asdict(record)
        with open(path, "a") as f:
            f.write(json.dumps(d, default=str) + "\n")

    def _write_log(self, record: RunRecord) -> None:
        log_path = self._dir / "logs" / f"{record.instance_id}.log"
        lines = []
        lines.append(f"instance_id: {record.instance_id}")
        lines.append(f"model: {record.model}")
        lines.append(f"exit_status: {record.exit_status}")
        lines.append(f"duration_s: {record.duration_s:.1f}")
        if record.error:
            lines.append(f"error: {record.error}")
        lines.append("--- transcript ---")
        for ev in record.transcript:
            lines.append(json.dumps(ev, default=str))
        with open(log_path, "w") as f:
            f.write("\n".join(lines) + "\n")

    def load_runs(self) -> list[RunRecord]:
        path = self._dir / "runs.jsonl"
        if not path.exists():
            return []
        records = []
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                d = json.loads(line)
                records.append(RunRecord(**{k: v for k, v in d.items() if k in RunRecord.__dataclass_fields__}))
        return records

    def close(self) -> None:
        pass


def default_run_id(model: str) -> str:
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H-%M-%S")
    return f"{ts}--{model}"


def default_output_dir() -> str:
    return os.path.expanduser("~/.cache/cece/benchmarks/results")