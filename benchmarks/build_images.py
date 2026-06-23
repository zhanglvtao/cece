"""Build SWE-bench env images (one per repo@version). No per-instance images."""

import argparse
import subprocess
import sys
from collections import defaultdict
from pathlib import Path
from typing import Optional


def _detect_platform() -> str:
    uname = subprocess.run(["uname", "-m"], capture_output=True, text=True).stdout.strip()
    return {"x86_64": "linux/amd64", "aarch64": "linux/arm64", "arm64": "linux/arm64"}.get(uname, f"linux/{uname}")


def _get_dockerfile(platform: str, ubuntu_version: str = "22.04",
                    conda_version: str = "py311_23.11.0-2") -> dict:
    conda_arch = "x86_64" if "amd64" in platform else "aarch64"

    base = f"""FROM --platform={platform} ubuntu:{ubuntu_version}

ARG DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

RUN apt update && apt install -y \\
wget git build-essential libffi-dev libtiff-dev python3 python3-pip \\
python-is-python3 jq curl locales locales-all tzdata \\
&& rm -rf /var/lib/apt/lists/*

RUN wget 'https://repo.anaconda.com/miniconda/Miniconda3-{conda_version}-Linux-{conda_arch}.sh' -O miniconda.sh \\
    && bash miniconda.sh -b -p /opt/miniconda3
ENV PATH=/opt/miniconda3/bin:$PATH
RUN conda init --all
RUN conda config --append channels conda-forge

RUN adduser --disabled-password --gecos 'dog' nonroot
"""

    env = """FROM {base_image}

COPY ./setup_env.sh /root/
RUN sed -i -e 's/\\r$//' /root/setup_env.sh
RUN chmod +x /root/setup_env.sh
RUN /bin/bash -c "source ~/.bashrc && /root/setup_env.sh"

WORKDIR /testbed/
RUN echo "source /opt/miniconda3/etc/profile.d/conda.sh && conda activate testbed" > /root/.bashrc
"""

    return {"base": base, "env": env}


def _build_image(image_name: str, dockerfile: str, build_dir: Path,
                 scripts: Optional[dict] = None, platform: str = "") -> None:
    build_dir.mkdir(parents=True, exist_ok=True)
    with open(build_dir / "Dockerfile", "w") as f:
        f.write(dockerfile)
    if scripts:
        for name, content in scripts.items():
            with open(build_dir / name, "w") as f:
                f.write(content)

    cmd = ["docker", "build", "-t", image_name, str(build_dir)]
    if platform:
        cmd += ["--platform", platform]

    print(f"  Building {image_name} ...")
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"  FAILED: {r.stderr[-500:]}")
        raise RuntimeError(f"Failed to build {image_name}")
    print(f"  Built {image_name}")


def _make_setup_env_script(inst: dict) -> str:
    repo = inst["repo"]
    lines = [
        "#!/bin/bash", "set -e",
        "source /opt/miniconda3/etc/profile.d/conda.sh",
        "conda create -n testbed python=3.9 -y || true",
        "conda activate testbed",
    ]

    installs = {
        "astropy/astropy": "pip install numpy pytest extension-helpers hypothesis pytest-astropy",
        "django/django": "pip install pytest",
        "sympy/sympy": "pip install pytest mpmath",
        "matplotlib/matplotlib": "pip install numpy pytest pillow hypothesis",
        "pallets/flask": "pip install pytest",
        "psf/black": "pip install pytest aiohttp",
        "scikit-learn/scikit-learn": "pip install numpy scipy pytest cython joblib threadpoolctl",
        "pylint-dev/pylint": "pip install pytest astroid isort",
        "pytest-dev/pytest": "pip install pytest",
        "pandas-dev/pandas": "pip install numpy pytest python-dateutil pytz",
        "mwaskom/seaborn": "pip install numpy pandas matplotlib pytest",
    }
    lines.append(installs.get(repo, "pip install pytest"))
    return "\n".join(lines) + "\n"


def get_env_image_for_instance(inst: dict) -> str:
    """Return the env image name for an instance."""
    repo = inst["repo"]
    version = inst.get("version", "")
    safe_repo = repo.replace("/", "_")
    return f"cece/sweb.env.{safe_repo}:{version or 'latest'}"


def build_env_images(dataset: str = "princeton-nlp/SWE-bench_Lite",
                     split: str = "test",
                     slice_str: Optional[str] = None,
                     force: bool = False) -> None:
    """Build ONE env image per unique repo@version."""
    try:
        from datasets import load_dataset
    except ImportError:
        print("ERROR: pip install datasets"); sys.exit(1)

    platform = _detect_platform()
    print(f"Platform: {platform}")

    ds = load_dataset(dataset, split=split)
    instances = list(ds)
    if slice_str:
        instances = eval(f"instances[{slice_str}]")

    groups: dict[str, list[dict]] = defaultdict(list)
    for inst in instances:
        key = f"{inst['repo']}@{inst.get('version', '')}"
        groups[key].append(inst)

    templates = _get_dockerfile(platform)
    cache_dir = Path.home() / ".cache" / "cece" / "benchmarks" / "images"
    built: set[str] = set()

    for key, group in sorted(groups.items()):
        repo = group[0]["repo"]
        version = group[0].get("version", "")
        safe_repo = repo.replace("/", "_")

        base_img = f"cece/sweb.base.{safe_repo}:{version or 'latest'}"
        env_img = f"cece/sweb.env.{safe_repo}:{version or 'latest'}"

        if not force:
            r = subprocess.run(["docker", "inspect", "--type=image", env_img], capture_output=True)
            if r.returncode == 0:
                print(f"  Skip (exists): {env_img}  ({len(group)} instances)")
                built.add(env_img)
                continue

        _build_image(base_img, templates["base"], cache_dir / "base" / safe_repo, platform=platform)

        env_scripts = {"setup_env.sh": _make_setup_env_script(group[0])}
        env_df = templates["env"].format(base_image=base_img)
        _build_image(env_img, env_df, cache_dir / "env" / safe_repo,
                     scripts=env_scripts, platform=platform)
        built.add(env_img)
        print(f"  → {env_img} covers {len(group)} instances\n")

    print(f"Done. {len(built)} env images for {len(instances)} instances.")


def main():
    p = argparse.ArgumentParser(description="Build SWE-bench env images (one per repo)")
    p.add_argument("--dataset", default="princeton-nlp/SWE-bench_Lite")
    p.add_argument("--split", default="test")
    p.add_argument("--slice", default=None)
    p.add_argument("--force", action="store_true")
    args = p.parse_args()
    build_env_images(args.dataset, args.split, args.slice, args.force)


if __name__ == "__main__":
    main()