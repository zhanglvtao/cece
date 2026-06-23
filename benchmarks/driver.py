"""IPC driver for cece engine — communicates via stdin/stdout JSONL."""

import json
import subprocess
import time
from dataclasses import dataclass, field
from typing import Iterator, Optional


@dataclass
class RunResult:
    exit_status: str       # "completed" | "timeout" | "error" | "run_failed"
    stats: Optional[dict] = None
    error: Optional[str] = None
    transcript: list[dict] = field(default_factory=list)


class CeceDriver:
    """Manages a cece engine process and communicates via JSONL IPC."""

    def __init__(self, proc: subprocess.Popen):
        self.proc = proc

    @classmethod
    def start(cls, exec_cmd: list[str], env: Optional[dict] = None) -> "CeceDriver":
        proc = subprocess.Popen(
            exec_cmd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
            env=env,
        )
        return cls(proc)

    def send(self, action: dict) -> None:
        if self.proc.stdin is None:
            raise RuntimeError("engine process has no stdin")
        line = json.dumps(action) + "\n"
        self.proc.stdin.write(line)
        self.proc.stdin.flush()

    def send_input(self, text: str) -> None:
        self.send({"type": "action", "kind": "input", "payload": {"text": text}})

    def send_confirm(self) -> None:
        self.send({"type": "action", "kind": "confirm"})

    def send_set_mode(self, mode: str) -> None:
        self.send({"type": "action", "kind": "set_permission_mode", "payload": {"mode": mode}})

    def events(self) -> Iterator[dict]:
        if self.proc.stdout is None:
            return
        for line in self.proc.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError:
                continue

    def wait_for_kind(self, kind: str, timeout: float) -> Optional[dict]:
        deadline = time.monotonic() + timeout
        for ev in self.events():
            if time.monotonic() > deadline:
                return None
            if ev.get("type") == "event" and ev.get("kind") == kind:
                return ev.get("payload", {})
        return None

    def run_until_done(self, input_text: str, timeout: float = 600) -> RunResult:
        self.send_input(input_text)
        deadline = time.monotonic() + timeout
        transcript: list[dict] = []
        exit_status = "unknown"
        error = None

        for ev in self.events():
            if time.monotonic() > deadline:
                exit_status = "timeout"
                break

            transcript.append(ev)

            if ev.get("type") == "error":
                error = ev.get("message", "unknown error")
                exit_status = "error"
                break

            kind = ev.get("kind", "")
            ptype = ev.get("type", "")

            if ptype == "event" and kind == "run_failed":
                payload = ev.get("payload", {})
                error = payload.get("err", payload.get("message", "unknown"))
                exit_status = "run_failed"
                break

            if ptype == "event" and kind == "turn_completed":
                exit_status = "completed"
                break

        stats = None
        if exit_status == "completed":
            self.send({"type": "action", "kind": "stats"})
            # drain remaining events until we get stats or timeout
            stats_deadline = time.monotonic() + 10
            for ev in self.events():
                if time.monotonic() > stats_deadline:
                    break
                transcript.append(ev)
                if ev.get("type") == "event" and ev.get("kind") == "stats":
                    payload = ev.get("payload", {})
                    stats = payload.get("Stats", payload)
                    break

        return RunResult(
            exit_status=exit_status,
            stats=stats,
            error=error,
            transcript=transcript,
        )

    def close(self) -> None:
        try:
            if self.proc.stdin:
                self.proc.stdin.close()
        except Exception:
            pass
        try:
            self.proc.terminate()
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait()