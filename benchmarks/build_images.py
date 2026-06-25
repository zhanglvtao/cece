"""Build SWE-bench env images (one per repo@version). No per-instance images."""

import argparse
import subprocess
import sys
import time
from collections import defaultdict
from pathlib import Path
from typing import Optional


BASE_UBUNTU_IMAGE = "registry-1.docker.io/library/ubuntu:22.04"


def _detect_platform() -> str:
    uname = subprocess.run(["uname", "-m"], capture_output=True, text=True).stdout.strip()
    return {"x86_64": "linux/amd64", "aarch64": "linux/arm64", "arm64": "linux/arm64"}.get(uname, f"linux/{uname}")


def platform_for_cece_binary(cece_bin: str) -> str:
    """Infer the Docker platform that can execute the selected cece binary."""
    path = Path(cece_bin).expanduser()
    if path.exists():
        return _platform_for_existing_binary(path)

    name = Path(cece_bin).name.lower()
    if "amd64" in name or "x86_64" in name:
        return "linux/amd64"
    if "arm64" in name or "aarch64" in name:
        return "linux/arm64"
    return _detect_platform()


def _platform_for_existing_binary(path: Path) -> str:
    r = subprocess.run(["file", str(path)], capture_output=True, text=True, check=False)
    if r.returncode != 0:
        raise RuntimeError(f"failed to inspect cece binary architecture: {path}")

    output = r.stdout.lower()
    if "elf" not in output:
        raise RuntimeError(f"cece binary is not a Linux ELF binary: {path} ({r.stdout.strip()})")
    if "x86-64" in output or "x86_64" in output or "amd64" in output:
        return "linux/amd64"
    if "aarch64" in output or "arm64" in output:
        return "linux/arm64"
    raise RuntimeError(f"unsupported cece binary architecture: {path} ({r.stdout.strip()})")


def _platform_slug(platform: str) -> str:
    return platform.replace("/", "_")


def _get_dockerfile(platform: str, base_image: str = BASE_UBUNTU_IMAGE,
                    conda_version: str = "py311_23.11.0-2") -> dict:
    conda_arch = "x86_64" if "amd64" in platform else "aarch64"

    base = f"""ARG TARGETPLATFORM
FROM --platform=$TARGETPLATFORM {base_image}

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

    print(f"  Building {image_name} ...", flush=True)
    r = subprocess.run(cmd)
    if r.returncode != 0:
        print(f"  FAILED: {image_name}", flush=True)
        raise RuntimeError(f"Failed to build {image_name}")
    print(f"  Built {image_name}", flush=True)


def _ensure_base_image_available(platform: str, image: str = BASE_UBUNTU_IMAGE,
                                 attempts: int = 2, timeout_s: int = 120) -> None:
    """Fail fast if Docker cannot resolve the Ubuntu base image."""
    if _image_exists_for_platform(image, platform):
        print(f"[prepare] base image exists locally: {image}", flush=True)
        return

    for attempt in range(1, attempts + 1):
        print(
            f"[prepare] pulling Docker base image ({attempt}/{attempts}): {image} for {platform}",
            flush=True,
        )
        try:
            r = subprocess.run(
                ["docker", "pull", "--platform", platform, image],
                timeout=timeout_s,
            )
        except subprocess.TimeoutExpired as e:
            print(f"[prepare] Docker base image pull timed out after {timeout_s}s", flush=True)
            if attempt == attempts:
                raise RuntimeError(_docker_base_image_error(image, platform, "timed out")) from e
            time.sleep(3)
            continue

        if r.returncode == 0:
            print(f"[prepare] base image ready: {image}", flush=True)
            return

        print(f"[prepare] Docker base image pull failed (rc={r.returncode})", flush=True)
        if attempt < attempts:
            time.sleep(3)

    raise RuntimeError(_docker_base_image_error(image, platform, "failed"))


def _docker_base_image_error(image: str, platform: str, reason: str) -> str:
    return (
        f"Cannot prepare SWE-bench Docker base image: {image} ({platform}) {reason}. "
        "Docker cannot reach the configured registry/mirror. In your Docker Desktop output this usually "
        "means the registry mirror (for example registry.docker-cn.com) is unavailable. Fix Docker registry "
        f"access or pre-pull the image manually with: docker pull --platform {platform} {image}"
    )


def _make_setup_env_script(inst: dict) -> str:
    repo = inst["repo"]
    lines = [
        "#!/bin/bash", "set -e",
        "source /opt/miniconda3/etc/profile.d/conda.sh",
        "conda create -n testbed python=3.9 -y || true",
        "conda activate testbed",
    ]

    installs = {
        "astropy/astropy": "pip install 'numpy<2' 'setuptools<58' cython 'pytest<8' extension-helpers hypothesis pytest-astropy 'pyerfa>=2.0'",
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


def get_env_image_for_instance(inst: dict, platform_name: Optional[str] = None) -> str:
    """Return the env image name for an instance."""
    repo = inst["repo"]
    version = inst.get("version", "")
    safe_repo = repo.replace("/", "_")
    platform_name = platform_name or _detect_platform()
    return f"cece/sweb.env.{safe_repo}.{_platform_slug(platform_name)}:{version or 'latest'}"


def ensure_env_images_for_instances(instances: list[dict], force: bool = False,
                                    platform_name: Optional[str] = None) -> None:
    """Ensure env images exist for the selected SWE-bench instances."""
    platform = platform_name or _detect_platform()
    print(f"[prepare] Docker platform: {platform}", flush=True)

    groups: dict[str, list[dict]] = defaultdict(list)
    for inst in instances:
        key = f"{inst['repo']}@{inst.get('version', '')}"
        groups[key].append(inst)

    templates = _get_dockerfile(platform)
    cache_dir = Path.home() / ".cache" / "cece" / "benchmarks" / "images"
    ready = 0
    built = 0
    needs_build = False

    for group in groups.values():
        env_img = get_env_image_for_instance(group[0], platform_name=platform)
        if force or not _image_exists(env_img):
            needs_build = True
            break

    if needs_build:
        _ensure_base_image_available(platform)

    for key, group in sorted(groups.items()):
        repo = group[0]["repo"]
        version = group[0].get("version", "")
        safe_repo = repo.replace("/", "_")
        base_img = f"cece/sweb.base.{safe_repo}.{_platform_slug(platform)}:{version or 'latest'}"
        env_img = get_env_image_for_instance(group[0], platform_name=platform)

        if not force and _image_exists(env_img):
            print(f"[prepare] env image exists: {env_img} ({len(group)} instances)", flush=True)
            ready += 1
            continue

        print(f"[prepare] env image missing: {env_img} ({len(group)} instances)", flush=True)
        _build_image(base_img, templates["base"], cache_dir / "base" / safe_repo, platform=platform)

        env_scripts = {"setup_env.sh": _make_setup_env_script(group[0])}
        env_df = templates["env"].format(base_image=base_img)
        _build_image(env_img, env_df, cache_dir / "env" / safe_repo,
                     scripts=env_scripts, platform=platform)
        built += 1
        print(f"[prepare] env image ready: {env_img}\n", flush=True)

    print(f"[prepare] SWE-bench env images ready: existing={ready}, built={built}, total={len(groups)}", flush=True)


def _image_exists(image_name: str) -> bool:
    r = subprocess.run(["docker", "inspect", "--type=image", image_name], capture_output=True)
    return r.returncode == 0


def _image_exists_for_platform(image_name: str, platform: str) -> bool:
    r = subprocess.run(
        ["docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image_name],
        capture_output=True,
        text=True,
    )
    return r.returncode == 0 and r.stdout.strip() == platform


def build_env_images(dataset: str = "princeton-nlp/SWE-bench_Lite",
                     split: str = "test",
                     slice_str: Optional[str] = None,
                     force: bool = False) -> None:
    """Build ONE env image per unique repo@version."""
    try:
        from datasets import load_dataset
    except ImportError:
        print("ERROR: pip install datasets"); sys.exit(1)

    ds = load_dataset(dataset, split=split)
    instances = list(ds)
    if slice_str:
        instances = eval(f"instances[{slice_str}]")
    ensure_env_images_for_instances(instances, force=force)


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
