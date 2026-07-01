"""SWE-bench scorer — scores in the same container used for running."""

import json
import subprocess
from typing import Callable, Optional

from .swebench_specs import (
    get_test_cmd,
    get_test_directives,
    parse_test_log,
    compute_resolution,
)


def _emit(log: Optional[Callable[[str], None]], message: str) -> None:
    if log:
        log(message)


def _tail_output(result: subprocess.CompletedProcess, limit: int = 1000) -> str:
    output = (result.stdout or "") + (result.stderr or "")
    return output[-limit:]


def _write_container_file(container_name: str, container_path: str, content: str,
                          timeout: int = 10) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["docker", "exec", "-i", container_name, "tee", container_path],
        input=content, capture_output=True, text=True, timeout=timeout,
    )


def _remove_container_file(container_name: str, container_path: str) -> None:
    subprocess.run(
        ["docker", "exec", container_name, "rm", "-f", container_path],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10,
    )


def _check_tracked_worktree_clean(container_name: str) -> subprocess.CompletedProcess:
    check_cmd = (
        "cd /testbed && "
        "if git diff --quiet HEAD -- .; then "
        "  exit 0; "
        "else "
        "  git status --porcelain --untracked-files=no; "
        "  exit 1; "
        "fi"
    )
    return subprocess.run(
        ["docker", "exec", container_name, "bash", "-c", check_cmd],
        capture_output=True, text=True, timeout=30,
    )


def _reset_worktree(container_name: str) -> subprocess.CompletedProcess:
    reset_cmd = (
        "cd /testbed && "
        "git reset --hard HEAD && "
        "git clean -fd -- . ':!.cece' ':!SYSTEM.md' ':!issue.md' 2>/dev/null || true"
    )
    return subprocess.run(
        ["docker", "exec", container_name, "bash", "-c", reset_cmd],
        capture_output=True, text=True, timeout=30,
    )


def _apply_patch_file(container_name: str, patch_path: str, timeout: int = 30,
                      check_only: bool = False) -> subprocess.CompletedProcess:
    if check_only:
        apply_cmd = f"cd /testbed && git apply --check {patch_path} 2>&1"
    else:
        apply_cmd = (
            f"cd /testbed && "
            f"git apply --check {patch_path} 2>&1 && "
            f"git apply {patch_path} 2>&1"
        )
    return subprocess.run(
        ["docker", "exec", container_name, "bash", "-c", apply_cmd],
        capture_output=True, text=True, timeout=timeout,
    )


def _build_test_command(inst: dict) -> str:
    repo = inst.get("repo", "")
    version = str(inst.get("version", ""))
    test_patch = inst.get("test_patch", "") or ""
    base_cmd = get_test_cmd(repo, version)
    directives = get_test_directives(repo, test_patch)
    return " ".join([base_cmd, *directives]).strip()


def preflight_instance(container_name: str, inst: dict,
                       log: Optional[Callable[[str], None]] = None) -> dict:
    test_patch = inst.get("test_patch", "") or ""
    if not test_patch.strip():
        _emit(log, "[preflight] SWE-bench test patch is missing")
        return {
            "status": "preflight_test_patch_missing",
            "category": "framework_preflight",
            "resolved": False,
        }

    _emit(log, "[preflight] checking tracked worktree is clean")
    try:
        clean_check = _check_tracked_worktree_clean(container_name)
    except subprocess.TimeoutExpired:
        _emit(log, "[preflight] tracked worktree clean check timed out")
        return {
            "status": "preflight_worktree_check_failed",
            "category": "framework_preflight",
            "resolved": False,
            "worktree_status": "timed out",
        }
    if clean_check.returncode != 0:
        detail = _tail_output(clean_check)
        _emit(log, "[preflight] tracked worktree is dirty before agent startup")
        return {
            "status": "preflight_dirty_worktree",
            "category": "framework_preflight",
            "resolved": False,
            "worktree_status": detail,
        }

    test_patch_bytes = len(test_patch.encode())
    test_patch_path = "/tmp/preflight_test_patch.diff"
    _emit(log, f"[preflight] writing SWE-bench test patch to {test_patch_path} ({test_patch_bytes} bytes)")
    try:
        test_patch_write = _write_container_file(
            container_name, test_patch_path, test_patch,
        )
        if test_patch_write.returncode != 0:
            detail = _tail_output(test_patch_write)
            _emit(log, f"[preflight] test patch write failed (rc={test_patch_write.returncode})")
            return {
                "status": "preflight_test_patch_write_failed",
                "category": "framework_preflight",
                "resolved": False,
                "test_patch_write_returncode": test_patch_write.returncode,
                "test_patch_write_output": detail,
                "test_patch_bytes": test_patch_bytes,
            }

        _emit(log, "[preflight] checking SWE-bench test patch on clean base")
        test_patch_check = _apply_patch_file(
            container_name, test_patch_path, check_only=True,
        )
        if test_patch_check.returncode != 0:
            detail = _tail_output(test_patch_check)
            _emit(log, f"[preflight] test patch apply check failed (rc={test_patch_check.returncode})")
            return {
                "status": "preflight_test_patch_apply_failed",
                "category": "framework_preflight",
                "resolved": False,
                "test_patch_apply_returncode": test_patch_check.returncode,
                "test_patch_apply_output": detail,
                "test_patch_bytes": test_patch_bytes,
            }

        fail_to_pass = _load_test_list(inst.get("FAIL_TO_PASS", "[]"))
        pass_to_pass = _load_test_list(inst.get("PASS_TO_PASS", "[]"))
        if not fail_to_pass:
            _emit(log, "[preflight] FAIL_TO_PASS is empty")
            return {
                "status": "preflight_no_fail_to_pass",
                "category": "framework_preflight",
                "resolved": False,
                "test_patch_bytes": test_patch_bytes,
                "fail_to_pass_count": 0,
                "pass_to_pass_count": len(pass_to_pass),
            }

        try:
            test_command = _build_test_command(inst)
        except Exception as e:
            _emit(log, f"[preflight] test command construction failed: {e}")
            return {
                "status": "preflight_command_failed",
                "category": "framework_preflight",
                "resolved": False,
                "test_patch_bytes": test_patch_bytes,
                "error": str(e),
            }
        if not test_command:
            _emit(log, "[preflight] test command is empty")
            return {
                "status": "preflight_command_failed",
                "category": "framework_preflight",
                "resolved": False,
                "test_patch_bytes": test_patch_bytes,
                "error": "empty test command",
            }

        _emit(log, f"[preflight] passed, test command: {test_command}")
        return {
            "status": "preflight_passed",
            "resolved": False,
            "test_command": test_command,
            "test_patch_bytes": test_patch_bytes,
            "fail_to_pass_count": len(fail_to_pass),
            "pass_to_pass_count": len(pass_to_pass),
        }
    except subprocess.TimeoutExpired:
        _emit(log, "[preflight] test patch check timed out")
        return {
            "status": "preflight_test_patch_apply_failed",
            "category": "framework_preflight",
            "resolved": False,
            "test_patch_apply_output": "timed out",
            "test_patch_bytes": test_patch_bytes,
        }
    finally:
        try:
            _remove_container_file(container_name, test_patch_path)
        except Exception:
            pass


def score_in_place(container_name: str, patch: str, inst: dict, timeout: int = 300,
                   log: Optional[Callable[[str], None]] = None) -> dict:
    patch_bytes = len(patch.encode())
    if not patch.strip():
        _emit(log, "[score] no patch to apply")
        return {"status": "no_patch", "resolved": False}

    _emit(log, f"[score] writing patch to /tmp/patch.diff ({patch_bytes} bytes)")
    write_result = _write_container_file(container_name, "/tmp/patch.diff", patch)
    if write_result.returncode != 0:
        detail = _tail_output(write_result)
        _emit(log, f"[score] patch write failed (rc={write_result.returncode})")
        return {
            "status": "patch_write_failed",
            "category": "scoring_error",
            "resolved": False,
            "patch_write_returncode": write_result.returncode,
            "patch_write_output": detail,
            "patch_bytes": patch_bytes,
        }

    _emit(log, "[score] resetting worktree and applying patch")
    reset_result = _reset_worktree(container_name)
    if reset_result.returncode != 0:
        detail = _tail_output(reset_result)
        _emit(log, f"[score] patch apply failed (rc={reset_result.returncode})")
        return {
            "status": "score_reset_failed",
            "category": "scoring_error",
            "resolved": False,
            "apply_returncode": reset_result.returncode,
            "apply_output": detail,
            "patch_bytes": patch_bytes,
        }

    apply_result = _apply_patch_file(container_name, "/tmp/patch.diff")
    if apply_result.returncode != 0:
        detail = _tail_output(apply_result)
        _emit(log, f"[score] patch apply failed (rc={apply_result.returncode})")
        return {
            "status": "score_apply_failed",
            "category": "scoring_error",
            "resolved": False,
            "apply_returncode": apply_result.returncode,
            "apply_output": detail,
            "patch_bytes": patch_bytes,
        }

    _emit(log, "[score] patch applied successfully")

    test_patch = inst.get("test_patch", "") or ""
    if test_patch.strip():
        test_patch_bytes = len(test_patch.encode())
        _emit(log, f"[score] writing SWE-bench test patch to /tmp/test_patch.diff ({test_patch_bytes} bytes)")
        test_patch_write = _write_container_file(
            container_name, "/tmp/test_patch.diff", test_patch,
        )
        if test_patch_write.returncode != 0:
            detail = _tail_output(test_patch_write)
            _emit(log, f"[score] test patch write failed (rc={test_patch_write.returncode})")
            return {
                "status": "test_patch_write_failed",
                "category": "scoring_error",
                "resolved": False,
                "test_patch_write_returncode": test_patch_write.returncode,
                "test_patch_write_output": detail,
                "patch_bytes": patch_bytes,
                "test_patch_bytes": test_patch_bytes,
            }

        _emit(log, "[score] applying SWE-bench test patch")
        test_patch_result = _apply_patch_file(container_name, "/tmp/test_patch.diff")
        if test_patch_result.returncode != 0:
            detail = _tail_output(test_patch_result)
            _emit(log, f"[score] model/test patch conflict (rc={test_patch_result.returncode})")
            return {
                "status": "model_test_patch_conflict",
                "category": "evaluation_conflict",
                "resolved": False,
                "test_patch_apply_returncode": test_patch_result.returncode,
                "test_patch_apply_output": detail,
                "patch_bytes": patch_bytes,
                "test_patch_bytes": test_patch_bytes,
            }

    fail_to_pass = _load_test_list(inst.get("FAIL_TO_PASS", "[]"))
    pass_to_pass = _load_test_list(inst.get("PASS_TO_PASS", "[]"))
    _emit(log, f"[score] loaded {len(fail_to_pass)} FAIL_TO_PASS, {len(pass_to_pass)} PASS_TO_PASS tests")

    test_cmd = _build_test_command(inst)
    full_cmd = (
        f"cd /testbed && "
        f"source /opt/miniconda3/etc/profile.d/conda.sh && "
        f"conda activate testbed && "
        f"{test_cmd}"
    )

    _emit(log, f"[score] running tests: {test_cmd}")
    try:
        r = subprocess.run(
            ["docker", "exec", container_name, "bash", "-c", full_cmd],
            capture_output=True, text=True, timeout=timeout,
        )
        output = r.stdout + r.stderr
    except subprocess.TimeoutExpired:
        _emit(log, f"[score] tests timed out after {timeout}s")
        return {
            "status": "timeout",
            "resolved": False,
            "test_command": test_cmd,
            "patch_bytes": patch_bytes,
        }

    status_map = parse_test_log(inst.get("repo", ""), output)
    resolution = compute_resolution(status_map, fail_to_pass, pass_to_pass)
    resolved = resolution["resolved"]
    status = "resolved" if resolved else "failed"
    _emit(
        log,
        f"[score] rc={r.returncode} status={status} "
        f"f2p={len(resolution['fail_to_pass_passed'])}/{len(fail_to_pass)} "
        f"p2p_failed={len(resolution['pass_to_pass_failed'])} "
        f"parsed={len(status_map)} tests",
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
    """Parse a FAIL_TO_PASS/PASS_TO_PASS field."""
    if isinstance(raw, list):
        return raw
    try:
        return json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return []
