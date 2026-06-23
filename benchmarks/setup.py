"""Auto-install benchmark dependencies into isolated venvs."""

import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Optional


CACHE_DIR = Path.home() / ".cache" / "cece" / "benchmarks"
VENVS_DIR = CACHE_DIR / "venvs"
REPOS_DIR = CACHE_DIR / "repos"
LOCK_FILE = CACHE_DIR / "setup-lock.json"


def _load_lock() -> dict:
    if LOCK_FILE.exists():
        return json.loads(LOCK_FILE.read_text())
    return {}


def _save_lock(lock: dict) -> None:
    LOCK_FILE.parent.mkdir(parents=True, exist_ok=True)
    LOCK_FILE.write_text(json.dumps(lock, indent=2))


def _ensure_venv(venv_dir: Path, deps: list[str]) -> str:
    python = venv_dir / "bin" / "python"
    if not python.exists():
        subprocess.run([sys.executable, "-m", "venv", str(venv_dir)], check=True)
    if deps:
        pip = venv_dir / "bin" / "pip"
        subprocess.run([str(pip), "install", "--quiet"] + deps, check=True)
    return str(python)


def _ensure_repo(name: str, url: str) -> Path:
    repo_dir = REPOS_DIR / name
    if repo_dir.exists():
        return repo_dir
    repo_dir.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["git", "clone", "--depth", "1", url, str(repo_dir)], check=True)
    return repo_dir


def setup_swebench() -> dict:
    venv_dir = VENVS_DIR / "swebench"
    python = _ensure_venv(venv_dir, deps=["datasets>=2.14.0", "docker"])
    repo = _ensure_repo("swebench", "https://github.com/swe-bench/SWE-bench.git")
    subprocess.run([python, "-m", "pip", "install", "--quiet", "-e", str(repo)], check=True)
    return {"python": python, "repo": str(repo)}


def setup_mswe() -> dict:
    venv_dir = VENVS_DIR / "mswe"
    python = _ensure_venv(venv_dir, deps=["datasets>=2.14.0", "docker"])
    repo = _ensure_repo("multi-swe-bench", "https://github.com/multi-swe-bench/multi-swe-bench.git")
    # multi-swe-bench uses make install
    subprocess.run(["make", "install"], cwd=str(repo), check=True,
                   env={**os.environ, "PATH": f"{venv_dir}/bin:{os.environ.get('PATH', '')}"})
    return {"python": python, "repo": str(repo)}


def setup_terminal_bench() -> dict:
    venv_dir = VENVS_DIR / "terminal-bench"
    python = _ensure_venv(venv_dir, deps=["terminal-bench"])
    return {"python": python, "repo": ""}


def setup_aider_polyglot() -> dict:
    venv_dir = VENVS_DIR / "aider-polyglot"
    python = _ensure_venv(venv_dir, deps=[])
    repo = _ensure_repo("aider", "https://github.com/Aider-AI/aider.git")
    subprocess.run([python, "-m", "pip", "install", "--quiet", "-e", str(repo) + "[dev]"], check=True)
    polyglot = _ensure_repo("polyglot-benchmark", "https://github.com/Aider-AI/polyglot-benchmark.git")
    return {"python": python, "repo": str(repo), "polyglot_dir": str(polyglot)}


def setup_spider2() -> dict:
    venv_dir = VENVS_DIR / "spider2"
    python = _ensure_venv(venv_dir, deps=["docker"])
    repo = _ensure_repo("spider2", "https://github.com/xlang-ai/Spider2.git")
    return {"python": python, "repo": str(repo)}


SETUP_FUNCTIONS = {
    "swebench": setup_swebench,
    "mswe": setup_mswe,
    "terminal-bench": setup_terminal_bench,
    "aider-polyglot": setup_aider_polyglot,
    "spider2": setup_spider2,
}


def setup_benchmark(name: str) -> Optional[dict]:
    fn = SETUP_FUNCTIONS.get(name)
    if fn is None:
        print(f"Unknown benchmark: {name}")
        return None
    print(f"Setting up {name}...")
    try:
        result = fn()
        lock = _load_lock()
        lock[name] = result
        _save_lock(lock)
        print(f"  Done: {name}")
        return result
    except Exception as e:
        print(f"  Failed: {e}")
        return None


def setup_all() -> dict[str, dict]:
    results = {}
    for name in SETUP_FUNCTIONS:
        r = setup_benchmark(name)
        if r:
            results[name] = r
    return results


def get_setup_info(name: str) -> Optional[dict]:
    return _load_lock().get(name)