"""SWE-bench scorer — scores in the same container used for running.

Scoring follows the official SWE-bench methodology:
  1. Run the repo's official test command over the test directives extracted
     from the test_patch (NOT a narrow -k filter).
  2. Parse the raw output with the repo's official log parser into a
     {test_id: STATUS} map.
  3. Resolve = all FAIL_TO_PASS pass AND all PASS_TO_PASS still pass.
"""

import json
import subprocess
from typing import Callable, Optional

from .swebench_specs import (
    get_test_cmd,
    get_test_directives,
    parse_test_log,
    compute_resolution,
)


def score_in_place(container_name: str, patch: str, inst: dict, timeout: int = 300,
                   log: Optional[Callable[[str], None]] = None) -> dict:
    """Apply patch and run FAIL_TO_PASS tests in an existing container.

    Called right after collect_artifact, before container is destroyed.
    """
    def emit(message: str) -> None:
        if log:
            log(message)

    patch_bytes = len(patch.encode())
    if not patch.strip():
        emit("[score] no patch to apply")
        return {"status": "no_patch", "resolved": False}

    # Write patch to container
    emit(f"[score] writing patch to /tmp/patch.diff ({patch_bytes} bytes)")
    write_result = subprocess.run(
        ["docker", "exec", "-i", container_name, "tee", "/tmp/patch.diff"],
        input=patch, capture_output=True, text=True, timeout=10,
    )
    if write_result.returncode != 0:
        detail = (write_result.stdout + write_result.stderr)[-1000:]
        emit(f"[score] patch write failed (rc={write_result.returncode})")
        return {
            "status": "patch_write_failed",
            "resolved": False,
            "patch_write_returncode": write_result.returncode,
            "patch_write_output": detail,
            "patch_bytes": patch_bytes,
        }

    # Reset working tree to clean state, then apply patch
    emit("[score] resetting worktree and applying patch")
    reset_and_apply = (
        "cd /testbed && "
        "git stash --include-untracked 2>/dev/null || true && "
        "git checkout -- . && "
        "git clean -fd -- . ':!.cece' 2>/dev/null || true && "
        "git apply --check /tmp/patch.diff 2>&1 && "
        "git apply /tmp/patch.diff 2>&1"
    )
    r = subprocess.run(
        ["docker", "exec", container_name, "bash", "-c", reset_and_apply],
        capture_output=True, text=True, timeout=30,
    )
    # Apply real failures surface as a nonzero return code (git apply --check
    # gates the chained apply). Do NOT scan output for "error": git prints benign
    # warnings like "warning: N lines add whitespace errors" on a successful apply.
    if r.returncode != 0:
        detail = (r.stdout + r.stderr)[-1000:]
        emit(f"[score] patch apply failed (rc={r.returncode})")
        return {
            "status": "apply_failed",
            "resolved": False,
            "apply_returncode": r.returncode,
            "apply_output": detail,
            "patch_bytes": patch_bytes,
        }

    emit("[score] patch applied successfully")

    test_patch = inst.get("test_patch", "") or ""
    if test_patch.strip():
        test_patch_bytes = len(test_patch.encode())
        emit(f"[score] writing SWE-bench test patch to /tmp/test_patch.diff ({test_patch_bytes} bytes)")
        test_patch_write = subprocess.run(
            ["docker", "exec", "-i", container_name, "tee", "/tmp/test_patch.diff"],
            input=test_patch, capture_output=True, text=True, timeout=10,
        )
        if test_patch_write.returncode != 0:
            detail = (test_patch_write.stdout + test_patch_write.stderr)[-1000:]
            emit(f"[score] test patch write failed (rc={test_patch_write.returncode})")
            return {
                "status": "test_patch_write_failed",
                "resolved": False,
                "test_patch_write_returncode": test_patch_write.returncode,
                "test_patch_write_output": detail,
                "patch_bytes": patch_bytes,
                "test_patch_bytes": test_patch_bytes,
            }

        emit("[score] applying SWE-bench test patch")
        apply_test_patch = (
            "cd /testbed && "
            "git apply --check /tmp/test_patch.diff 2>&1 && "
            "git apply /tmp/test_patch.diff 2>&1"
        )
        test_patch_result = subprocess.run(
            ["docker", "exec", container_name, "bash", "-c", apply_test_patch],
            capture_output=True, text=True, timeout=30,
        )
        if test_patch_result.returncode != 0:
            detail = (test_patch_result.stdout + test_patch_result.stderr)[-1000:]
            emit(f"[score] test patch apply failed (rc={test_patch_result.returncode})")
            return {
                "status": "test_patch_apply_failed",
                "resolved": False,
                "test_patch_apply_returncode": test_patch_result.returncode,
                "test_patch_apply_output": detail,
                "patch_bytes": patch_bytes,
                "test_patch_bytes": test_patch_bytes,
            }

    # Parse FAIL_TO_PASS / PASS_TO_PASS test identifiers
    fail_to_pass = _load_test_list(inst.get("FAIL_TO_PASS", "[]"))
    pass_to_pass = _load_test_list(inst.get("PASS_TO_PASS", "[]"))
    emit(f"[score] loaded {len(fail_to_pass)} FAIL_TO_PASS, {len(pass_to_pass)} PASS_TO_PASS tests")

    repo = inst.get("repo", "")
    version = str(inst.get("version", ""))
    test_patch = inst.get("test_patch", "") or ""

    # Build the official test command: <test_cmd> <directives...>
    base_cmd = get_test_cmd(repo, version)
    directives = get_test_directives(repo, test_patch)
    test_cmd = " ".join([base_cmd, *directives]).strip()

    full_cmd = (
        f"cd /testbed && "
        f"source /opt/miniconda3/etc/profile.d/conda.sh && "
        f"conda activate testbed && "
        f"{test_cmd}"
    )

    emit(f"[score] running tests: {test_cmd}")
    try:
        r = subprocess.run(
            ["docker", "exec", container_name, "bash", "-c", full_cmd],
            capture_output=True, text=True, timeout=timeout,
        )
        output = r.stdout + r.stderr
    except subprocess.TimeoutExpired:
        emit(f"[score] tests timed out after {timeout}s")
        return {
            "status": "timeout",
            "resolved": False,
            "test_command": test_cmd,
            "patch_bytes": patch_bytes,
        }

    # Parse with the repo's official log parser, then apply F2P + P2P resolution.
    status_map = parse_test_log(repo, output)
    resolution = compute_resolution(status_map, fail_to_pass, pass_to_pass)
    resolved = resolution["resolved"]
    status = "resolved" if resolved else "failed"
    emit(
        f"[score] rc={r.returncode} status={status} "
        f"f2p={len(resolution['fail_to_pass_passed'])}/{len(fail_to_pass)} "
        f"p2p_failed={len(resolution['pass_to_pass_failed'])} "
        f"parsed={len(status_map)} tests"
    )
    return {
        "status": status,
        "resolved": resolved,
        "test_command": test_cmd,
        "test_returncode": r.returncode,
        "test_output": output[-2000:],
        "patch_bytes": patch_bytes,
        "fail_to_pass_count": len(fail_to_pass),
        "pass_to_pass_count": len(pass_to_pass),
        "fail_to_pass_failed": resolution["fail_to_pass_failed"],
        "pass_to_pass_failed": resolution["pass_to_pass_failed"],
    }


def _load_test_list(raw) -> list[str]:
    """Parse a FAIL_TO_PASS/PASS_TO_PASS field (JSON string or list)."""
    if isinstance(raw, list):
        return raw
    try:
        return json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return []
