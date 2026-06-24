"""SWE-bench scorer — scores in the same container used for running."""

import json
import subprocess


def score_in_place(container_name: str, patch: str, inst: dict, timeout: int = 300) -> dict:
    """Apply patch and run FAIL_TO_PASS tests in an existing container.

    Called right after collect_artifact, before container is destroyed.
    """
    if not patch.strip():
        return {"status": "no_patch", "resolved": False}

    # Write patch to container
    subprocess.run(
        ["docker", "exec", "-i", container_name, "tee", "/tmp/patch.diff"],
        input=patch, capture_output=True, text=True, timeout=10,
    )

    # Reset working tree to clean state, then apply patch
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
        return {"status": "apply_failed", "resolved": False, "detail": (r.stdout + r.stderr)[-300:]}

    # Parse FAIL_TO_PASS test identifiers
    fail_to_pass_raw = inst.get("FAIL_TO_PASS", "[]")
    try:
        fail_to_pass = json.loads(fail_to_pass_raw) if isinstance(fail_to_pass_raw, str) else fail_to_pass_raw
    except (json.JSONDecodeError, TypeError):
        fail_to_pass = []

    # Build test command
    is_django = any("(" in t and ")" in t for t in fail_to_pass)
    if is_django:
        # Django tests: find manage.py or use django test runner
        # Django test IDs are like "test_name (module.Class)"
        django_tests = []
        for t in fail_to_pass:
            # Parse "test_name (module.Class)" → "module.Class.test_name"
            if "(" in t and ")" in t:
                paren = t[t.index("(") + 1:t.index(")")]
                test_name = t[:t.index("(")].strip()
                if "." in paren:
                    django_tests.append(f"{paren}.{test_name}")
                else:
                    django_tests.append(t)
            else:
                django_tests.append(t)
        test_labels = " ".join(django_tests)
        test_cmd = (
            f"if [ -f manage.py ]; then "
            f"python manage.py test {test_labels} --verbosity=2 2>&1; "
            f"elif [ -f tests/manage.py ]; then "
            f"cd tests && python manage.py test {test_labels} --verbosity=2 2>&1; "
            f"else "
            f"python -m django test {test_labels} --verbosity=2 2>&1; "
            f"fi; echo 'EXIT_CODE=$?'"
        )
    elif fail_to_pass:
        # For pytest, strip parametrized brackets and use -k for filtering
        # e.g. "test_separable[compound_model6-result6]" → -k "test_separable"
        test_names = set()
        for t in fail_to_pass:
            base = t.split("[")[0] if "[" in t else t
            test_names.add(base)
        # Try running specific test files if path-like, else use -k
        if any("/" in t or ".py" in t for t in fail_to_pass):
            escaped = " ".join(f'"{t}"' for t in fail_to_pass)
            test_cmd = f"pytest {escaped} --tb=short -q 2>&1; echo 'EXIT_CODE=$?'"
        else:
            # Use -k for parametrized / named tests
            k_args = " or ".join(test_names)
            test_cmd = f"pytest -k '{k_args}' --tb=short -q 2>&1; echo 'EXIT_CODE=$?'"
    else:
        test_cmd = "pytest --tb=short -q 2>&1; echo 'EXIT_CODE=$?'"

    full_cmd = (
        f"cd /testbed && "
        f"source /opt/miniconda3/etc/profile.d/conda.sh && "
        f"conda activate testbed && "
        f"{test_cmd}"
    )

    try:
        r = subprocess.run(
            ["docker", "exec", container_name, "bash", "-c", full_cmd],
            capture_output=True, text=True, timeout=timeout,
        )
        output = r.stdout + r.stderr
    except subprocess.TimeoutExpired:
        return {"status": "timeout", "resolved": False}

    passed = _parse_test_output(output)
    return {
        "status": "resolved" if passed else "failed",
        "resolved": passed,
        "test_output": output[-500:],
    }


def _parse_test_output(output: str) -> bool:
    lower = output.lower()

    # Definitely failed
    if any(kw in lower for kw in ["failed", "error", "traceback", "importerror"]):
        if "0 failed" in lower and "passed" in lower:
            return True
        if "no tests ran" in lower:
            return False
        return False

    # Success markers
    if "EXIT_CODE=0" in output:
        if "no tests ran" in lower:
            return False
        return True

    if "0 failed" in lower and "passed" in lower:
        return True

    return False