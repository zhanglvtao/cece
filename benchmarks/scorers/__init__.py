"""Scorer base."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field


@dataclass
class ScoreReport:
    total: int = 0
    resolved: int = 0
    pass_rate: float = 0.0
    details: list[dict] = field(default_factory=list)


class Scorer(ABC):
    name: str

    @abstractmethod
    def score(self, result_dir: str, dataset: str, split: str) -> ScoreReport:
        ...