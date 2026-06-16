"""IPC driver for cece engine — communicates via stdin/stdout JSONL."""

import json
import subprocess
import time
from typing import Optional


class CeceDriver:
    """Manages a cece engine process and communicates via JSONL IPC."""

    def __init__(self, proc: subprocess.Popen):
        self.proc = proc
        self._last_event: Optional[dict] = None

    @classmethod
    def start(cls, cece_bin: str, project_dir: str) -> "CeceDriver":
        """Start cece engine process."""
        proc = subprocess.Popen(
            [cece_bin, "engine", "--project-dir", project_dir],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        return cls(proc)

    def send(self, action: dict) -> None:
        """Send a JSONL action to the engine."""
        if self.proc.stdin is None:
            raise RuntimeError("engine process has no stdin")
        line = json.dumps(action) + "\n"
        self.proc.stdin.write(line)
        self.proc.stdin.flush()

    def send_input(self, text: str) -> None:
        """Send user input to the engine."""
        self.send(
            {"type": "action", "kind": "input", "payload": {"text": text}}
        )

    def send_confirm(self) -> None:
        """Send confirm action (approve tool calls)."""
        self.send({"type": "action", "kind": "confirm"})

    def get_stats(self) -> Optional[dict]:
        """Request and receive session statistics."""
        self.send({"type": "action", "kind": "stats"})
        return self._wait_for_kind("stats", timeout=10)

    def events(self):
        """Iterate over JSONL events from engine stdout."""
        if self.proc.stdout is None:
            return
        for line in self.proc.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue
            self._last_event = msg
            yield msg

    def _wait_for_kind(self, kind: str, timeout: float) -> Optional[dict]:
        """Wait for a specific event kind. Returns None on timeout."""
        deadline = time.monotonic() + timeout
        for ev in self.events():
            if time.monotonic() > deadline:
                return None
            if ev.get("type") == "event" and ev.get("kind") == kind:
                return ev.get("payload", {})
        return None

    def run_until_done(self, input_text: str, timeout: float = 600) -> dict:
        """Send input, wait for TurnCompleted (or timeout/error), return stats.

        Returns dict with keys: patch, stats, error, exit_status.
        """
        self.send_input(input_text)
        deadline = time.monotonic() + timeout

        exit_status = "unknown"
        error = None

        for ev in self.events():
            if time.monotonic() > deadline:
                exit_status = "timeout"
                break

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

        stats = self.get_stats() if exit_status == "completed" else None

        return {
            "exit_status": exit_status,
            "error": error,
            "stats": stats,
        }

    def close(self) -> None:
        """Terminate the engine process."""
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