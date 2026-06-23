"""Build SWE-bench/MSWE Docker images for the host architecture (arm64 native)."""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def _detect_platform() -> str:
    """Detect host architecture for Docker builds."""
    uname = subprocess.run(["uname", "-m"], capture_output=True, text=True).stdout.strip()
    arch_map = {
        "x86_64": "linux/amd64",
        "aarch64": "linux/arm64",
        "arm64": "linux/arm64",
    }
    return arch_map.get(uname, f"linux/{uname}")


def _get_dockerfile_python(platform: str, ubuntu_version: str = "22.04",
                           conda_version: str = "py311_23.11.0-2") -> dict:
    """Return the 3-layer Python dockerfile templates from SWE-bench."""
    # Determine conda arch for the platform
    conda_arch = "x86_64" if "amd64" in platform else "aarch64"

    base = f"""FROM --platform={platform} ubuntu:{ubuntu_version}

ARG DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

RUN apt update && apt install -y \\
wget \\
git \\
build-essential \\
libffi-dev \\
libtiff-dev \\
python3 \\
python3-pip \\
python-is-python3 \\
jq \\
curl \\
locales \\
locales-all \\
tzdata \\
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

    instance = """FROM {env_image}

COPY ./setup_repo.sh /root/
RUN sed -i -e 's/\\r$//' /root/setup_repo.sh
RUN /bin/bash /root/setup_repo.sh

WORKDIR /testbed/
"""

    return {"base": base, "env": env, "instance": instance}


def _build_image(image_name: str, dockerfile: str, build_dir: Path,
                 setup_scripts: dict | None = None, platform: str = "") -> None:
    """Build a single Docker image."""
    build_dir.mkdir(parents=True, exist_ok=True)

    with open(build_dir / "Dockerfile", "w") as f:
        f.write(dockerfile)

    if setup_scripts:
        for name, content in setup_scripts.items():
            with open(build_dir / name, "w") as f:
                f.write(content)

    cmd = ["docker", "build", "-t", image_name, str(build_dir)]
    if platform:
        cmd += ["--platform", platform]

    print(f"  Building {image_name} ...")
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  FAILED: {result.stderr[-500:]}")
        raise RuntimeError(f"Failed to build {image_name}")
    print(f"  Built {image_name}")


def build_swebench_images(dataset: str = "princeton-nlp/SWE-bench_Lite",
                          split: str = "test",
                          slice_str: str | None = None,
                          max_workers: int = 4,
                          force: bool = False) -> None:
    """Build SWE-bench instance images for host architecture."""
    try:
        from datasets import load_dataset
    except ImportError:
        print("ERROR: pip install datasets")
        sys.exit(1)

    platform = _detect_platform()
    print(f"Host platform: {platform}")
    print(f"Dataset: {dataset} ({split})")

    ds = load_dataset(dataset, split=split)
    instances = list(ds)
    if slice_str:
        instances = eval(f"instances[{slice_str}]")

    print(f"Instances to build: {len(instances)}")

    templates = _get_dockerfile_python(platform)

    # Group by (repo, version) to share env images
    from collections import defaultdict
    env_groups: dict[str, list[dict]] = defaultdict(list)
    for inst in instances:
        repo = inst["repo"]
        version = inst.get("version", "")
        env_key = f"{repo}@{version}"
        env_groups[env_key].append(inst)

    cache_dir = Path.home() / ".cache" / "cece" / "benchmarks" / "images"
    built_base: set[str] = set()
    built_env: set[str] = set()

    for env_key, group in env_groups.items():
        repo = group[0]["repo"]
        version = group[0].get("version", "")

        # Base image
        base_image = f"cece/sweb.base.{repo.replace('/', '_')}:{version or 'latest'}"
        if force or base_image not in built_base:
            _build_image(base_image, templates["base"],
                        cache_dir / "base" / repo.replace("/", "_"),
                        platform=platform)
            built_base.add(base_image)

        # Env image — get setup_env from instance data
        env_image = f"cece/sweb.env.{repo.replace('/', '_')}:{version or 'latest'}"
        if force or env_image not in built_env:
            inst0 = group[0]
            setup_env = inst0.get("environment_setup", "")
            # SWE-bench stores env setup commands; construct a script
            env_scripts = {"setup_env.sh": _make_setup_env_script(inst0)}
            env_df = templates["env"].format(base_image=base_image)
            _build_image(env_image, env_df,
                        cache_dir / "env" / repo.replace("/", "_"),
                        setup_scripts=env_scripts, platform=platform)
            built_env.add(env_image)

        # Instance images
        for inst in group:
            iid = inst["instance_id"]
            inst_image = f"cece/sweb.inst.{iid.replace('__', '_')}:latest"
            if not force:
                # Check if exists
                r = subprocess.run(["docker", "inspect", "--type=image", inst_image],
                                   capture_output=True)
                if r.returncode == 0:
                    print(f"  Skip (exists): {inst_image}")
                    continue

            setup_repo = _make_setup_repo_script(inst)
            inst_df = templates["instance"].format(env_image=env_image)
            _build_image(inst_image, inst_df,
                        cache_dir / "instances" / iid.replace("__", "_"),
                        setup_scripts={"setup_repo.sh": setup_repo},
                        platform=platform)

    print(f"\nDone. Built images for {len(instances)} instances.")
    print(f"Platform: {platform}")
    print(f"\nTo run: python -m benchmarks run swebench --slice {slice_str or ':all'}")


def _make_setup_env_script(inst: dict) -> str:
    """Create environment setup script from SWE-bench instance data."""
    repo = inst["repo"]
    version = inst.get("version", "")
    # Try to get install instructions from the instance
    env_setup_commit = inst.get("environment_setup_commit", "")

    lines = [
        "#!/bin/bash",
        "set -e",
        "source /opt/miniconda3/etc/profile.d/conda.sh",
        "",
        "# Create testbed environment",
        "conda create -n testbed python=3.9 -y || true",
        "conda activate testbed",
        "",
        f"# Environment for {repo}",
    ]

    # Common deps for Python repos
    if any(x in repo.lower() for x in ["django", "flask", "scikit", "sympy", "astropy"]):
        lines.append("pip install pytest")

    # Repo-specific installs
    repo_installs = {
        "astropy/astropy": "pip install numpy pytest extension-helpers",
        "django/django": "pip install pytest",
        "sympy/sympy": "pip install pytest mpmath",
        "matplotlib/matplotlib": "pip install numpy pytest pillow",
        "pallets/flask": "pip install pytest",
        "psf/black": "pip install pytest aiohttp",
        "scikit-learn/scikit-learn": "pip install numpy scipy pytest cython joblib threadpoolctl",
        "pylint-dev/pylint": "pip install pytest astroid isort",
        "pytest-dev/pytest": "pip install pytest",
        "pandas-dev/pandas": "pip install numpy pytest python-dateutil pytz",
        "mwaskom/seaborn": "pip install numpy pandas matplotlib pytest",
        "django/django": "pip install pytest",
    }

    install_cmd = repo_installs.get(repo, "pip install -e .")
    lines.append(install_cmd)

    return "\n".join(lines) + "\n"


def _make_setup_repo_script(inst: dict) -> str:
    """Create repo checkout + install script from SWE-bench instance data."""
    repo = inst["repo"]
    base_commit = inst["base_commit"]

    return f"""#!/bin/bash
set -e
source /opt/miniconda3/etc/profile.d/conda.sh
conda activate testbed

cd /testbed

if [ -d ".git" ]; then
    git fetch origin
else
    git init
    git remote add origin https://github.com/{repo}.git
fi

git checkout {base_commit}

# Install the repo
pip install -e . 2>/dev/null || echo "Install may need custom deps"

echo "Repo ready at /testbed"
"""


def main():
    parser = argparse.ArgumentParser(description="Build SWE-bench Docker images for host arch")
    parser.add_argument("--dataset", default="princeton-nlp/SWE-bench_Lite")
    parser.add_argument("--split", default="test")
    parser.add_argument("--slice", default=None, help="e.g. ':10' for first 10")
    parser.add_argument("--max-workers", type=int, default=4)
    parser.add_argument("--force", action="store_true", help="Rebuild existing images")
    args = parser.parse_args()

    build_swebench_images(
        dataset=args.dataset,
        split=args.split,
        slice_str=args.slice,
        max_workers=args.max_workers,
        force=args.force,
    )


if __name__ == "__main__":
    main()