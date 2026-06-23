"""Multi-SWE-bench adapter."""

import json
import subprocess
from pathlib import Path
from typing import Optional

from ..auth import resolve_auth_tokens
from ..prompts import mswe as mswe_prompt
from . import BenchmarkAdapter, Sandbox


class MSWEBenchAdapter(BenchmarkAdapter):
    name = "mswe"
    requires_docker = True
    default_timeout = 600

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        try:
            from datasets import load_dataset
        except ImportError:
            raise ImportError("pip install datasets")
        ds = load_dataset(dataset, split=split)
        instances = list(ds)
        if slice:
            instances = eval(f"instances[{slice}]")
        return instances

    def instance_id(self, inst: dict) -> str:
        return f"{inst.get('org', '')}__{inst.get('repo', '')}__{inst.get('number', '')}"

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        org = inst["org"]
        repo = inst["repo"]
        number = str(inst["number"])
        problem_statement = inst.get("problem_statement", inst.get("issue", ""))
        base_commit = inst.get("base_commit", "HEAD")

        # MSWE uses Docker images named: sweb.eval.x86_64.{org}_1776_{repo}-{number}
        docker_id = f"{org}__{repo}__{number}".replace("__", "_1776_")
        image = f"docker.io/swebench/sweb.eval.x86_64.{docker_id}:latest".lower()
        container_name = f"cece-mswe-{org}-{repo}-{number}"

        subprocess.run(["docker", "pull", image], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(["docker", "rm", "-f", container_name],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["docker", "run", "-d", "--name", container_name, image, "sleep", "infinity"],
                       check=True, stdout=subprocess.DEVNULL)

        subprocess.run(["docker", "cp", cece_bin, f"{container_name}:/usr/local/bin/cece"], check=True)
        subprocess.run(["docker", "exec", container_name, "chmod", "+x", "/usr/local/bin/cece"], check=True)

        host_config = {**config}
        host_config["provider"]["model"] = config.get("model", "deepseek-v4-pro")
        host_config["defaultMode"] = {"mode": "plan"}
        host_config["yolo"] = {"enabled": True}
        host_config = resolve_auth_tokens(host_config)

        self._write_file(container_name, "/testbed/.cece/settings.json", json.dumps(host_config, indent=2))
        self._write_file(container_name, "/testbed/SYSTEM.md", mswe_prompt.TEMPLATE)
        self._write_file(container_name, "/testbed/issue.md", problem_statement)

        self._exec(container_name, ["git", "checkout", base_commit], workdir="/testbed")
        self._exec(container_name, ["git", "config", "user.email", "cece@mswe.local"], workdir="/testbed")
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
            extra={"container_name": container_name, "org": org, "repo": repo, "number": number},
        )

    def build_prompt(self, inst: dict) -> str:
        problem_statement = inst.get("problem_statement", inst.get("issue", ""))
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
        org = sandbox.extra["org"]
        repo = sandbox.extra["repo"]
        number = sandbox.extra["number"]
        return {
            "patch": patch,
            "org": org,
            "repo": repo,
            "number": number,
            "fix_patch": patch,
        }

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