"""cece benchmarks — unified evaluation suite."""

from .adapters.swebench import SWEBenchAdapter
from .adapters.mswe import MSWEBenchAdapter
from .adapters.terminal_bench import TerminalBenchAdapter
from .adapters.aider_polyglot import AiderPolyglotAdapter
from .adapters.spider2 import Spider2Adapter

from .scorers.mswe import MSWEBenchScorer
from .scorers.terminal_bench import TerminalBenchScorer
from .scorers.aider_polyglot import AiderPolyglotScorer
from .scorers.spider2 import Spider2Scorer

ADAPTERS = {
    "swebench": SWEBenchAdapter,
    "mswe": MSWEBenchAdapter,
    "terminal-bench": TerminalBenchAdapter,
    "aider-polyglot": AiderPolyglotAdapter,
    "spider2": Spider2Adapter,
}

# SWE-bench uses in-place scoring (score_in_place function, not a class)
SCORERS = {
    "mswe": MSWEBenchScorer,
    "terminal-bench": TerminalBenchScorer,
    "aider-polyglot": AiderPolyglotScorer,
    "spider2": Spider2Scorer,
}


def get_adapter(name: str):
    cls = ADAPTERS.get(name)
    if cls is None:
        raise ValueError(f"Unknown benchmark: {name}. Available: {list(ADAPTERS.keys())}")
    return cls()


def get_scorer(name: str):
    cls = SCORERS.get(name)
    if cls is None:
        raise ValueError(f"Unknown benchmark scorer: {name}. Available: {list(SCORERS.keys())}")
    return cls()


def list_benchmarks() -> list[str]:
    return list(ADAPTERS.keys())


def list_scorers() -> list[str]:
    return list(SCORERS.keys())
