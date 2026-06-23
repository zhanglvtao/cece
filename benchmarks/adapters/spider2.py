"""Spider2 adapter — runs Spider2 tasks with cece as the agent."""

import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Optional

from . import BenchmarkAdapter, Sandbox


class Spider2Adapter(BenchmarkAdapter):
    name = "spider2"
    requires_docker = True
    default_timeout = 600

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        # Spider2 dataset is a JSONL file distributed via the repo.
        # dataset param is the path to the JSONL file or a preset name.
        preset_paths = {
            "spider2-snow": "spider2-snow.jsonl",
            "spider2-lite": "spider2-lite.jsonl",
        }
        path = preset_paths.get(dataset, dataset)
        instances = []
        if os.path.exists(path):
            with open(path) as f:
                for line in f:
                    line = line.strip()
                    if line:
                        instances.append(json.loads(line))
        if slice:
            instances = eval(f"instances[{slice}]")
        return instances

    def instance_id(self, inst: dict) -> str:
        return inst.get("question_id", inst.get("id", str(hash(inst.get("question", "")))))

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        workdir = tempfile.mkdtemp(prefix="cece-spider2-")

        # Write config
        settings_path = os.path.join(workdir, ".cece", "settings.json")
        os.makedirs(os.path.dirname(settings_path), exist_ok=True)
        with open(settings_path, "w") as f:
            json.dump(config, f)

        # Write spider2 task info
        with open(os.path.join(workdir, "task.json"), "w") as f:
            json.dump(inst, f)

        exec_cmd = [cece_bin, "engine", "--project-dir", workdir]

        def cleanup():
            subprocess.run(["rm", "-rf", workdir], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        return Sandbox(
            kind="local",
            project_dir=workdir,
            exec_cmd=exec_cmd,
            cleanup=cleanup,
            extra={"workdir": workdir, "inst": inst},
        )

    def build_prompt(self, inst: dict) -> str:
        question = inst.get("question", inst.get("instruction", ""))
        db_info = inst.get("db_id", inst.get("database", ""))
        evidence = inst.get("evidence", "")
        return (
            "You are a text-to-SQL expert. Write a SQL query to answer the following question.\n\n"
            f"Database: {db_info}\n"
            f"Question: {question}\n"
            + (f"Evidence/Context: {evidence}\n" if evidence else "")
            + "\nOutput ONLY the SQL query, nothing else."
        )

    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        # Read the agent's output from the transcript — the last assistant delta
        # In practice this is handled by the runner extracting SQL from the transcript.
        return {"answer": ""}