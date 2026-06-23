"""Aider polyglot adapter — runs aider benchmark harness with cece as the model."""

import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Optional

from . import BenchmarkAdapter, Sandbox


class AiderPolyglotAdapter(BenchmarkAdapter):
    name = "aider-polyglot"
    requires_docker = True
    default_timeout = 300

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        # Aider polyglot exercises are managed by the polyglot-benchmark repo.
        # Return a single synthetic instance.
        return [{"dataset": dataset, "split": split, "slice": slice}]

    def instance_id(self, inst: dict) -> str:
        return f"polyglot-{inst.get('slice', 'all')}"

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        workdir = tempfile.mkdtemp(prefix="cece-aider-")

        def cleanup():
            subprocess.run(["rm", "-rf", workdir], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        return Sandbox(
            kind="docker",
            project_dir=workdir,
            exec_cmd=[],
            cleanup=cleanup,
            extra={"cece_bin": cece_bin, "config": config, "workdir": workdir},
        )

    def build_prompt(self, inst: dict) -> str:
        return ""

    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        return {}


def run_cece_on_exercise(exercise_dir: str, cece_bin: str, config: dict, timeout: int) -> dict:
    """Run cece on a single aider polyglot exercise."""
    from ..driver import CeceDriver

    settings_path = os.path.join(exercise_dir, ".cece", "settings.json")
    os.makedirs(os.path.dirname(settings_path), exist_ok=True)
    with open(settings_path, "w") as f:
        json.dump(config, f)

    proc = subprocess.Popen(
        [cece_bin, "engine", "--project-dir", exercise_dir],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )
    driver = CeceDriver(proc)

    instruction_file = os.path.join(exercise_dir, "instructions.md")
    instruction = ""
    if os.path.exists(instruction_file):
        with open(instruction_file) as f:
            instruction = f.read()

    result = driver.run_until_done(instruction, timeout=timeout)
    driver.close()

    # Run tests
    test_result = ""
    try:
        test_result = subprocess.run(
            ["python", "-m", "pytest", exercise_dir, "-q"],
            capture_output=True, text=True, timeout=60,
            cwd=exercise_dir,
        ).stdout
    except Exception:
        pass

    return {
        "exit_status": result.exit_status,
        "transcript": result.transcript,
        "stats": result.stats,
        "error": result.error,
        "test_output": test_result,
        "passed": "failed" not in test_result.lower() if test_result else False,
    }