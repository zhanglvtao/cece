"""SWE-bench scorer — scores in the same container used for running."""

import json
import re
import shlex
import subprocess
from typing import Callable, Optional


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
    if r.returncode != 0 or "error" in r.stdout.lower() or "error" in r.stderr.lower():
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

    # Parse FAIL_TO_PASS test identifiers
    fail_to_pass_raw = inst.get("FAIL_TO_PASS", "[]")
    try:
        fail_to_pass = json.loads(fail_to_pass_raw) if isinstance(fail_to_pass_raw, str) else fail_to_pass_raw
    except (json.JSONDecodeError, TypeError):
        fail_to_pass = []
    emit(f"[score] loaded {len(fail_to_pass)} FAIL_TO_PASS tests")

    test_kind, test_cmd = _build_test_command(fail_to_pass)

    full_cmd = (
        f"cd /testbed && "
        f"source /opt/miniconda3/etc/profile.d/conda.sh && "
        f"conda activate testbed && "
        f"{test_cmd}"
    )

    emit(f"[score] running {test_kind} tests: {test_cmd}")
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
            "test_kind": test_kind,
            "test_command": test_cmd,
            "patch_bytes": patch_bytes,
        }

    passed = _parse_test_output(output, r.returncode)
    status = "resolved" if passed else "failed"
    emit(f"[score] tests finished rc={r.returncode} status={status}")
    return {
        "status": status,
        "resolved": passed,
        "test_kind": test_kind,
        "test_command": test_cmd,
        "test_returncode": r.returncode,
        "test_output": output[-2000:],
        "patch_bytes": patch_bytes,
        "fail_to_pass_count": len(fail_to_pass),
    }


def _parse_test_output(output: str, returncode: Optional[int] = None) -> bool:
    lower = output.lower()

    explicit_exit = _extract_exit_code(output)
    if explicit_exit is not None:
        return explicit_exit == 0

    if "no tests ran" in lower:
        return False

    if returncode == 0:
        return True

    # Definitely failed
    if any(kw in lower for kw in ["failed", "error", "traceback", "importerror"]):
        if "0 failed" in lower and "passed" in lower:
            return True
        return False

    # Success markers
    if "EXIT_CODE=0" in output:
        return True

    if "0 failed" in lower and "passed" in lower:
        return True

    return False


def _build_test_command(fail_to_pass: list[str]) -> tuple[str, str]:
    test_kind = "pytest_all"
    pytest_opts = "-p no:cacheprovider --tb=short -q -W ignore -W ignore::DeprecationWarning --disable-pytest-warnings"
    is_django = any("(" in t and ")" in t for t in fail_to_pass)
    if is_django:
        test_kind = "django"
        django_tests = [_django_test_label(t) for t in fail_to_pass]
        test_labels = " ".join(shlex.quote(t) for t in django_tests)
        test_cmd = (
            f"if [ -f tests/runtests.py ]; then "
            f"python tests/runtests.py {test_labels} --verbosity=2 2>&1; "
            f"rc=$?; "
            f"elif [ -f manage.py ]; then "
            f"python manage.py test {test_labels} --verbosity=2 2>&1; "
            f"rc=$?; "
            f"elif [ -f tests/manage.py ]; then "
            f"cd tests && python manage.py test {test_labels} --verbosity=2 2>&1; "
            f"rc=$?; "
            f"else "
            f"python -m django test {test_labels} --verbosity=2 2>&1; "
            f"rc=$?; "
            f"fi; echo EXIT_CODE=$rc; exit $rc"
        )
    elif fail_to_pass:
        test_kind = "pytest_selected"
        if any("/" in t or ".py" in t for t in fail_to_pass):
            test_ids = " ".join(shlex.quote(t) for t in fail_to_pass)
            files, names = _pytest_file_and_name_filters(fail_to_pass)
            escaped_files = " ".join(shlex.quote(f) for f in files)
            if names:
                k_args = " or ".join(sorted(names))
                fallback_cmd = f"pytest {escaped_files} -k {shlex.quote(k_args)} {pytest_opts} 2>&1"
            else:
                fallback_cmd = f"pytest {escaped_files} {pytest_opts} 2>&1"
            test_cmd = (
                f"tmp=$(mktemp); "
                f"pytest {test_ids} {pytest_opts} >$tmp 2>&1; rc=$?; cat $tmp; "
                f"if [ $rc -eq 4 ] || [ $rc -eq 5 ] || grep -Eqi 'no tests ran|not found' $tmp; then "
                f"echo '[score] exact pytest nodeids selected no tests; falling back to file/function filters'; "
                f"{fallback_cmd}; rc=$?; "
                f"fi; "
                f"rm -f $tmp; echo EXIT_CODE=$rc; exit $rc"
            )
        else:
            test_names = set()
            for t in fail_to_pass:
                base = t.split("[")[0] if "[" in t else t
                test_names.add(base)
            k_args = " or ".join(test_names)
            test_cmd = f"pytest -k {shlex.quote(k_args)} {pytest_opts} 2>&1; rc=$?; echo EXIT_CODE=$rc; exit $rc"
    else:
        test_cmd = f"pytest {pytest_opts} 2>&1; rc=$?; echo EXIT_CODE=$rc; exit $rc"

    return test_kind, test_cmd


def _django_test_label(test_id: str) -> str:
    if "(" in test_id and ")" in test_id:
        paren = test_id[test_id.index("(") + 1:test_id.index(")")]
        test_name = test_id[:test_id.index("(")].strip()
        if "." in paren:
            return f"{paren}.{test_name}"
    return test_id


def _pytest_file_and_name_filters(test_ids: list[str]) -> tuple[list[str], set[str]]:
    files: set[str] = set()
    names: set[str] = set()
    for test_id in test_ids:
        file_part, _, node_part = test_id.partition("::")
        files.add(file_part)
        if node_part:
            func = node_part.split("[")[0].split("::")[-1]
            if func:
                names.add(func)
    return sorted(files), names


def _extract_exit_code(output: str) -> Optional[int]:
    matches = re.findall(r"EXIT_CODE=(\d+)", output)
    if not matches:
        return None
    return int(matches[-1])
