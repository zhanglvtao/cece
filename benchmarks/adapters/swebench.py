"""SWE-bench adapter."""

import json
import subprocess
from pathlib import Path
from typing import Optional

from ..auth import resolve_auth_tokens
from ..prompts import swebench as swebench_prompt
from . import BenchmarkAdapter, Sandbox


class SWEBenchAdapter(BenchmarkAdapter):
    name = "swebench"
    requires_docker = True
    default_timeout = 600

    def __init__(self):
        self._instances: list[dict] = []

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        try:
            from datasets import load_dataset
        except ImportError:
            raise ImportError("pip install datasets")
        ds = load_dataset(dataset, split=split)
        instances = list(ds)
        if slice:
            instances = eval(f"instances[{slice}]")
        self._instances = instances
        return instances

    def _inst_index(self, inst: dict) -> int:
        try:
            return self._instances.index(inst)
        except ValueError:
            return 0

    def instance_id(self, inst: dict) -> str:
        return inst["instance_id"]

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        instance_id = inst["instance_id"]
        base_commit = inst["base_commit"]
        problem_statement = inst["problem_statement"]

        container_name = f"cece-swebench-{instance_id.replace('__', '-')}"

        # 1. Try local native arch image first (built by build_images.py)
        local_image = f"cece/sweb.inst.{instance_id.replace('__', '_')}:latest"
        image = None

        r = subprocess.run(["docker", "inspect", "--type=image", local_image],
                           capture_output=True)
        if r.returncode == 0:
            image = local_image

        # 2. Fall back to official SWE-bench image (may be x86_64 + QEMU)
        if not image:
            docker_id = instance_id.replace("__", "_1776_")
            official_image = f"docker.io/swebench/sweb.eval.x86_64.{docker_id}:latest".lower()
            try:
                subprocess.run(["docker", "pull", official_image], check=True, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, timeout=120)
                image = official_image
            except Exception:
                pass

        if not image:
            raise RuntimeError(
                f"No image found for {instance_id}. "
                f"Run: python -m benchmarks.build_images --slice :{self._inst_index(inst)} --dataset <ds>"
            )

        # Remove existing container
        subprocess.run(["docker", "rm", "-f", container_name],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # Start container
        subprocess.run(["docker", "run", "-d", "--name", container_name, image, "sleep", "infinity"],
                       check=True, stdout=subprocess.DEVNULL)

        # Copy cece binary (must match container arch)
        subprocess.run(["docker", "cp", cece_bin, f"{container_name}:/usr/local/bin/cece"], check=True)
        subprocess.run(["docker", "exec", container_name, "chmod", "+x", "/usr/local/bin/cece"], check=True)

        # Prepare config
        host_config = {**config}
        host_config["provider"]["model"] = config.get("model", "deepseek-v4-pro")
        host_config["defaultMode"] = {"mode": "plan"}
        host_config["yolo"] = {"enabled": True}
        host_config = resolve_auth_tokens(host_config)

        self._write_file(container_name, "/testbed/.cece/settings.json", json.dumps(host_config, indent=2))
        self._write_file(container_name, "/testbed/SYSTEM.md", swebench_prompt.TEMPLATE)
        self._write_file(container_name, "/testbed/issue.md", problem_statement)

        # Checkout base commit
        self._exec(container_name, ["git", "checkout", base_commit], workdir="/testbed")
        self._exec(container_name, ["git", "config", "user.email", "cece@swebench.local"], workdir="/testbed")
        self._exec(container_name, ["git", "config", "user.name", "cece"], workdir="/testbed")

        exec_cmd = ["docker", "exec", "-i", container_name, "/usr/local/bin/cece", "engine", "--project-dir", "/testbed"]

        def cleanup():
            subprocess.run(["docker", "rm", "-f", container_name],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        return Sandbox(
            kind="docker",
            project_dir="/testbed",
            exec_cmd=exec_cmd,
            cleanup=cleanup,
            extra={"container_name": container_name},
        )

    def build_prompt(self, inst: dict) -> str:
        problem_statement = inst["problem_statement"]
        return (
            f"Please fix the following GitHub issue in the /testbed codebase.\n\n"
            f"<issue>\n{problem_statement}\n</issue>"
        )

    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        container_name = sandbox.extra["container_name"]
        self._exec(container_name, ["git", "add", "-A"], workdir="/testbed")
        self._exec(container_name, ["git", "reset", "--", ".cece/", "SYSTEM.md", "issue.md"], workdir="/testbed")
        patch = self._exec(container_name,
                           ["git", "--no-pager", "diff", "--cached", "--binary", "--no-ext-diff"],
                           workdir="/testbed", capture=True)
        return {"patch": patch}

    def _write_file(self, container_name: str, container_path: str, content: str) -> None:
        subprocess.run(
            ["docker", "exec", container_name, "mkdir", "-p", str(Path(container_path).parent)],
            check=True,
        )
        subprocess.run(
            ["docker", "exec", "-i", container_name, "tee", container_path],
            input=content, check=True, stdout=subprocess.DEVNULL, text=True,
        )

    def _exec(self, container_name: str, cmd: list[str], workdir: str = "/testbed", capture: bool = False) -> str:
        docker_cmd = ["docker", "exec", "-w", workdir, container_name] + cmd
        if capture:
            result = subprocess.run(docker_cmd, capture_output=True, text=True)
            return result.stdout
        else:
            subprocess.run(docker_cmd, check=True)
            return ""