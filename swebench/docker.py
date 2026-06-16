"""Docker container management for SWE-bench instances."""

import json
import os
import subprocess
import time
from pathlib import Path
from typing import Optional


class DockerInstance:
    """Manage a SWE-bench Docker container for one evaluation instance."""

    def __init__(
        self,
        instance_id: str,
        cece_bin: str,
        model: str,
        api_key: str,
        base_url: str = "https://api.anthropic.com",
        protocol: str = "anthropic",
        image_tag: str = "latest",
    ):
        self.instance_id = instance_id
        self.cece_bin = Path(cece_bin).resolve()
        self.model = model
        self.api_key = api_key
        self.base_url = base_url
        self.protocol = protocol

        docker_id = instance_id.replace("__", "_1776_")
        self.image = f"docker.io/swebench/sweb.eval.x86_64.{docker_id}:{image_tag}".lower()
        self.container_name = f"cece-swebench-{instance_id.replace('__', '-')}"

    def pull_image(self) -> None:
        """Pull the SWE-bench instance image."""
        subprocess.run(
            ["docker", "pull", self.image],
            check=True,
            stdout=subprocess.DEVNULL,
        )

    def start(self, base_commit: str, problem_statement: str) -> None:
        """Start container and prepare environment."""
        # Remove existing container if any
        subprocess.run(
            ["docker", "rm", "-f", self.container_name],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Start container
        subprocess.run(
            ["docker", "run", "-d", "--name", self.container_name, self.image, "sleep", "infinity"],
            check=True,
            stdout=subprocess.DEVNULL,
        )

        # Copy cece binary
        subprocess.run(
            ["docker", "cp", str(self.cece_bin), f"{self.container_name}:/usr/local/bin/cece"],
            check=True,
        )
        subprocess.run(
            ["docker", "exec", self.container_name, "chmod", "+x", "/usr/local/bin/cece"],
            check=True,
        )

        # Write settings.json
        settings = {
            "provider": {
                "model": self.model,
                "maxTokens": 16384,
                "providers": [{
                    "name": "api",
                    "protocol": self.protocol,
                    "apiKey": self.api_key,
                    "baseURL": self.base_url,
                }],
            },
            "defaultMode": {"mode": "auto-accept"},
            "yolo": {"enabled": True},
        }
        self._write_file("/testbed/.cece/settings.json", json.dumps(settings, indent=2))

        # Write SYSTEM.md
        from swebench.prompt import TEMPLATE
        self._write_file("/testbed/SYSTEM.md", TEMPLATE)

        # Write issue.md
        self._write_file("/testbed/issue.md", problem_statement)

        # Checkout base commit
        self._exec(["git", "checkout", base_commit], workdir="/testbed")

        # Git config for diff
        self._exec(["git", "config", "user.email", "cece@swebench.local"], workdir="/testbed")
        self._exec(["git", "config", "user.name", "cece"], workdir="/testbed")

    def exec_command(self, cmd: list[str], workdir: str = "/testbed") -> str:
        """Run a command in the container and return stdout."""
        return self._exec(cmd, workdir=workdir, capture=True)

    def get_patch(self) -> str:
        """Get git diff of all changes in the container."""
        self._exec(["git", "add", "-A"], workdir="/testbed")
        return self._exec(
            ["git", "--no-pager", "diff", "--cached", "--binary", "--no-ext-diff"],
            workdir="/testbed",
            capture=True,
        )

    def stop(self) -> None:
        """Stop and remove the container."""
        subprocess.run(
            ["docker", "rm", "-f", self.container_name],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    def _write_file(self, container_path: str, content: str) -> None:
        """Write content to a file inside the container."""
        subprocess.run(
            ["docker", "exec", self.container_name, "mkdir", "-p", str(Path(container_path).parent)],
            check=True,
        )
        subprocess.run(
            ["docker", "exec", "-i", self.container_name, "tee", container_path],
            input=content,
            check=True,
            stdout=subprocess.DEVNULL,
            text=True,
        )

    def _exec(self, cmd: list[str], workdir: str = "/testbed", capture: bool = False) -> str:
        """Execute a command inside the container."""
        docker_cmd = ["docker", "exec", "-w", workdir, self.container_name] + cmd
        if capture:
            result = subprocess.run(docker_cmd, capture_output=True, text=True)
            return result.stdout
        else:
            subprocess.run(docker_cmd, check=True)
            return ""

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.stop()