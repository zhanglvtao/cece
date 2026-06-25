"""SWE-bench adapter — runtime init from env image, run+score in same container."""

import json
import os
import subprocess
from pathlib import Path
from typing import Optional

from ..auth import resolve_auth_tokens
from ..build_images import get_env_image_for_instance, platform_for_cece_binary
from ..prompts import swebench as swebench_prompt
from . import BenchmarkAdapter, Sandbox


class SWEBenchAdapter(BenchmarkAdapter):
    name = "swebench"
    requires_docker = True
    default_timeout = 600

    def __init__(self):
        self._instances: list[dict] = []

    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        from datasets import load_dataset
        ds = load_dataset(dataset, split=split)
        instances = list(ds)
        if slice:
            instances = eval(f"instances[{slice}]")
        self._instances = instances
        return instances

    def instance_id(self, inst: dict) -> str:
        return inst["instance_id"]

    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        instance_id = inst["instance_id"]
        repo = inst["repo"]
        base_commit = inst["base_commit"]
        problem_statement = inst["problem_statement"]
        container_name = f"cece-swebench-{instance_id.replace('__', '-')}"

        # Resolve env image
        env_image = get_env_image_for_instance(inst, platform_name=platform_for_cece_binary(cece_bin))
        r = subprocess.run(["docker", "inspect", "--type=image", env_image], capture_output=True)
        if r.returncode != 0:
            raise RuntimeError(
                f"Env image not found after automatic preparation: {env_image}"
            )

        # Clean up old container
        subprocess.run(["docker", "rm", "-f", container_name],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # Start from env image
        subprocess.run(
            ["docker", "run", "-d", "--name", container_name, env_image, "sleep", "infinity"],
            check=True, stdout=subprocess.DEVNULL,
        )

        # Copy cece binary
        subprocess.run(["docker", "cp", cece_bin, f"{container_name}:/usr/local/bin/cece"], check=True)
        subprocess.run(["docker", "exec", container_name, "chmod", "+x", "/usr/local/bin/cece"], check=True)

        # Init repo: fetch only the requested base commit. Avoid cloning the
        # default branch first; large repos such as astropy can exceed the
        # sandbox setup timeout when a full working tree is cloned up front.
        # Add retry logic for unstable networks
        self._exec(container_name, [
            "bash", "-c",
            f"cd /testbed && "
            f"if [ ! -d '.git' ]; then "
            f"  git init && "
            f"  git remote add origin https://github.com/{repo}.git; "
            f"  git config http.version HTTP/1.1; "
            f"  git config core.compression 0; "
            f"fi && "
            f"git config http.version HTTP/1.1 && "
            f"git config core.compression 0 && "
            f"for i in 1 2 3; do "
            f"  echo \"Attempt $i/3: git fetch --depth 1 origin {base_commit}\" && "
            f"  git fetch --depth 1 origin {base_commit} && "
            f"  if [ $? -eq 0 ]; then break; fi; "
            f"  sleep 5; "
            f"done; "
            f"git checkout --force FETCH_HEAD"
        ], workdir="/testbed", timeout=600)

        # Install dependencies (separate step to avoid monolith timeout)
        # setuptools<58 and cython should be in env image; skip if already present
        self._exec(container_name, [
            "bash", "-c",
            f"source /opt/miniconda3/etc/profile.d/conda.sh && "
            f"conda activate testbed && "
            f"python -c 'import setuptools; assert int(setuptools.__version__.split(\".\")[0]) < 58' 2>/dev/null || pip install 'setuptools<58' wheel 2>&1 && "
            f"python -c 'import Cython' 2>/dev/null || pip install cython 2>&1 && "
            f"python setup.py develop 2>&1 || pip install . 2>&1 || true"
        ], workdir="/testbed", timeout=900)

        # Git config
        self._exec(container_name, [
            "bash", "-c",
            f"git config user.email cece@swebench.local && "
            f"git config user.name cece"
        ], workdir="/testbed", timeout=30)

        # Write config + prompt files
        host_config = {**config}
        model = config.get("model", "gpt-5.5-paygo")
        host_config["provider"]["model"] = model
        # Auto-select provider based on model prefix
        if "/" in model:
            provider_name = model.split("/")[0]
            host_config["provider"]["defaultProvider"] = provider_name
        host_config["defaultMode"] = {"mode": "plan"}
        host_config["yolo"] = {"enabled": True}
        host_config = resolve_auth_tokens(host_config)

        self._write_file(container_name, "/testbed/.cece/settings.json", json.dumps(host_config, indent=2))
        self._write_file(container_name, "/testbed/SYSTEM.md", swebench_prompt.TEMPLATE)
        self._write_file(container_name, "/testbed/issue.md", problem_statement)

        exec_cmd = ["docker", "exec", "-i"]
        if os.environ.get("TRAECLI_TOKEN"):
            exec_cmd.extend(["-e", f"TRAECLI_TOKEN={os.environ['TRAECLI_TOKEN']}"])
        exec_cmd.extend([container_name, "/usr/local/bin/cece", "engine", "--project-dir", "/testbed"])

        def cleanup():
            subprocess.run(["docker", "rm", "-f", container_name],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        return Sandbox(
            kind="docker",
            project_dir="/testbed",
            exec_cmd=exec_cmd,
            cleanup=cleanup,
            extra={"container_name": container_name, "instance": inst},
        )

    def build_prompt(self, inst: dict) -> str:
        return (
            f"Please fix the following GitHub issue in the /testbed codebase.\n\n"
            f"<issue>\n{inst['problem_statement']}\n</issue>"
        )

    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        container_name = sandbox.extra["container_name"]
        self._exec(container_name, ["git", "add", "-A"], workdir="/testbed")
        self._exec(container_name, ["git", "reset", "--", ".cece/", "SYSTEM.md", "issue.md"], workdir="/testbed")
        patch = self._exec(container_name,
                           [
                               "git", "--no-pager", "diff", "--cached", "--binary", "--no-ext-diff", "--",
                               ":(exclude).cece/", ":(exclude)SYSTEM.md", ":(exclude)issue.md",
                               ":(exclude)build/", ":(exclude)dist/", ":(exclude)*.egg-info/",
                               ":(exclude)*.zip", ":(exclude)*.whl", ":(exclude)*.egg",
                               ":(exclude)*.tar", ":(exclude)*.tar.gz", ":(exclude)*.tgz",
                               ":(exclude)*.tar.bz2", ":(exclude)*.tar.xz",
                               ":(exclude)*.gz", ":(exclude)*.bz2", ":(exclude)*.xz",
                           ],
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

    def _exec(self, container_name: str, cmd: list[str], workdir: str = "/testbed",
              capture: bool = False, timeout: int = 300) -> str:
        docker_cmd = ["docker", "exec", "-w", workdir, container_name] + cmd
        result = subprocess.run(docker_cmd, capture_output=True, text=True, timeout=timeout)
        if result.returncode != 0:
            raise RuntimeError(
                f"Command failed (rc={result.returncode}): {' '.join(cmd)}\n"
                f"stderr: {result.stderr[:2000]}"
            )
        return result.stdout
