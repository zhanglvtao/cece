"""Terminal-bench adapter — wraps tb CLI with cece as the agent."""

import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Optional

from . import BenchmarkAdapter, Sandbox


class TerminalBenchAdapter(BenchmarkAdapter):
    name = "terminal-bench"
    requires_docker = True
    default_timeout = 900

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        # Terminal-bench tasks are managed by tb CLI; we return a single synthetic instance
        # representing the dataset filter. Actual task enumeration happens in run_until_done via tb.
        return [{"dataset": dataset, "split": split, "slice": slice}]

    def instance_id(self, inst: dict) -> str:
        return f"tb-{inst['dataset']}-{inst.get('slice', 'all')}"

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        # Terminal-bench manages its own Docker sandbox; cece runs inside it.
        # We create a temp working dir and use tb run with a custom agent adapter.
        workdir = tempfile.mkdtemp(prefix="cece-tb-")

        def cleanup():
            subprocess.run(["rm", "-rf", workdir], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        return Sandbox(
            kind="docker",
            project_dir=workdir,
            exec_cmd=[],  # Terminal-bench uses tb CLI directly, not cece engine
            cleanup=cleanup,
            extra={"cece_bin": cece_bin, "config": config, "workdir": workdir},
        )

    def build_prompt(self, inst: dict) -> str:
        return ""  # Terminal-bench tasks have their own instructions

    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        return {}  # Artifact collected during run via tb output


def run_cece_on_tb_task(task_dir: str, cece_bin: str, config: dict, timeout: int) -> dict:
    """Run cece on a single Terminal-bench task directory."""
    from ..driver import CeceDriver

    # Write config to task dir
    settings_path = os.path.join(task_dir, ".cece", "settings.json")
    os.makedirs(os.path.dirname(settings_path), exist_ok=True)
    with open(settings_path, "w") as f:
        json.dump(config, f)

    proc = subprocess.Popen(
        [cece_bin, "engine", "--project-dir", task_dir],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )
    driver = CeceDriver(proc)

    # Read task instruction
    instruction_file = os.path.join(task_dir, "instruction.md")
    instruction = ""
    if os.path.exists(instruction_file):
        with open(instruction_file) as f:
            instruction = f.read()

    result = driver.run_until_done(instruction, timeout=timeout)
    driver.close()
    return {
        "exit_status": result.exit_status,
        "transcript": result.transcript,
        "stats": result.stats,
        "error": result.error,
    }