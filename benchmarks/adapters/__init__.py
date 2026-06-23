"""Benchmark adapter base."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any, Callable, Optional


@dataclass
class Sandbox:
    kind: str                    # "docker" | "local"
    project_dir: str             # cece --project-dir
    exec_cmd: list[str]          # ["docker", "exec", "-i", container, "cece", "engine", "--project-dir", "/testbed"]
    env: Optional[dict[str, str]] = None
    cleanup: Callable[[], None] = field(default=lambda: None)
    extra: dict[str, Any] = field(default_factory=dict)  # adapter-specific data


class BenchmarkAdapter(ABC):
    name: str
    requires_docker: bool = True
    default_timeout: int = 600

    @abstractmethod
    def load_instances(self, dataset: str, split: str, slice: Optional[str] = None) -> list[dict]:
        ...

    @abstractmethod
    def instance_id(self, inst: dict) -> str:
        ...

    @abstractmethod
    def setup_sandbox(self, inst: dict, cece_bin: str, config: dict) -> Sandbox:
        ...

    def teardown_sandbox(self, sandbox: Sandbox) -> None:
        sandbox.cleanup()

    @abstractmethod
    def build_prompt(self, inst: dict) -> str:
        ...

    @abstractmethod
    def collect_artifact(self, sandbox: Sandbox, inst: dict) -> dict:
        ...