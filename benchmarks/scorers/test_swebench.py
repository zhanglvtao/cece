from benchmarks.scorers.swebench import (
    _load_test_list,
    preflight_instance,
    score_in_place,
)
from benchmarks.scorers.swebench_specs import (
    TEST_ASTROPY_PYTEST,
    TEST_DJANGO,
    TEST_PYTEST,
    compute_resolution,
    get_test_cmd,
    get_test_directives,
    parse_test_log,
)


def test_get_test_cmd_uses_repo_specific_command():
    assert get_test_cmd("astropy/astropy", "5.1") == TEST_ASTROPY_PYTEST
    assert get_test_cmd("django/django", "4.1") == TEST_DJANGO
    assert get_test_cmd("sympy/sympy", "1.10").startswith("PYTHONWARNINGS=")
    # Unknown repo falls back to plain pytest.
    assert get_test_cmd("some/unknown", "") == TEST_PYTEST


def test_get_test_cmd_version_override_for_django_1_9():
    assert get_test_cmd("django/django", "1.9") == "./tests/runtests.py --verbosity 2"


def test_get_test_directives_extracts_test_files():
    test_patch = (
        "diff --git a/astropy/io/fits/tests/test_table.py b/astropy/io/fits/tests/test_table.py\n"
        "--- a/astropy/io/fits/tests/test_table.py\n"
        "+++ b/astropy/io/fits/tests/test_table.py\n"
        "@@ -1 +1 @@\n-x\n+y\n"
    )
    assert get_test_directives("astropy/astropy", test_patch) == [
        "astropy/io/fits/tests/test_table.py"
    ]


def test_get_test_directives_django_converts_to_module_path():
    test_patch = (
        "diff --git a/tests/auth_tests/test_views.py b/tests/auth_tests/test_views.py\n"
        "@@ -1 +1 @@\n-x\n+y\n"
    )
    assert get_test_directives("django/django", test_patch) == ["auth_tests.test_views"]


def test_get_test_directives_skips_non_test_files():
    test_patch = (
        "diff --git a/docs/changes/1.rst b/docs/changes/1.rst\n@@ -1 +1 @@\n-x\n+y\n"
    )
    assert get_test_directives("astropy/astropy", test_patch) == []


def test_parse_test_log_astropy_classic_style():
    log = (
        "astropy/io/fits/tests/test_table.py::TestTable::test_ascii PASSED\n"
        "astropy/io/fits/tests/test_table.py::TestTable::test_other FAILED\n"
    )
    status = parse_test_log("astropy/astropy", log)
    assert status["astropy/io/fits/tests/test_table.py::TestTable::test_ascii"] == "PASSED"
    assert status["astropy/io/fits/tests/test_table.py::TestTable::test_other"] == "FAILED"


def test_compute_resolution_requires_all_f2p_and_p2p():
    status = {"t_fix": "PASSED", "t_keep": "PASSED"}
    res = compute_resolution(status, ["t_fix"], ["t_keep"])
    assert res["resolved"] is True

    # F2P fails -> not resolved.
    status_fail = {"t_fix": "FAILED", "t_keep": "PASSED"}
    assert compute_resolution(status_fail, ["t_fix"], ["t_keep"])["resolved"] is False

    # P2P regression -> not resolved.
    status_regress = {"t_fix": "PASSED", "t_keep": "FAILED"}
    assert compute_resolution(status_regress, ["t_fix"], ["t_keep"])["resolved"] is False


def test_compute_resolution_empty_p2p_only_needs_f2p():
    status = {"t_fix": "PASSED"}
    assert compute_resolution(status, ["t_fix"], [])["resolved"] is True
    assert compute_resolution({"t_fix": "FAILED"}, ["t_fix"], [])["resolved"] is False


def test_load_test_list_handles_json_string_and_list():
    assert _load_test_list('["a", "b"]') == ["a", "b"]
    assert _load_test_list(["a", "b"]) == ["a", "b"]
    assert _load_test_list("not json") == []


def test_preflight_passes_when_test_patch_applies_on_base(monkeypatch):
    calls = []

    def fake_run(cmd, **kwargs):
        calls.append(cmd)
        return _completed(returncode=0)

    monkeypatch.setattr("benchmarks.scorers.swebench.subprocess.run", fake_run)
    inst = _swebench_inst()

    result = preflight_instance("container", inst)

    assert result["status"] == "preflight_passed"
    assert result["test_command"] == f"{TEST_PYTEST} tests/test_example.py"
    assert result["fail_to_pass_count"] == 1
    assert any("git apply --check /tmp/preflight_test_patch.diff" in cmd[-1] for cmd in calls)
    assert not any("git apply /tmp/preflight_test_patch.diff" in cmd[-1] for cmd in calls if cmd[-2:] != ["tee", "/tmp/preflight_test_patch.diff"])
    assert ["docker", "exec", "container", "rm", "-f", "/tmp/preflight_test_patch.diff"] in calls


def test_preflight_fails_before_agent_when_test_patch_does_not_apply(monkeypatch):
    def fake_run(cmd, **kwargs):
        if cmd[:5] == ["docker", "exec", "container", "bash", "-c"] and "git diff --quiet HEAD -- ." in cmd[-1]:
            return _completed(returncode=0)
        if cmd[-2:] == ["tee", "/tmp/preflight_test_patch.diff"]:
            return _completed(returncode=0)
        if cmd[:5] == ["docker", "exec", "container", "bash", "-c"] and "git apply --check /tmp/preflight_test_patch.diff" in cmd[-1]:
            return _completed(returncode=1, stderr="error: patch does not apply")
        return _completed(returncode=0)

    monkeypatch.setattr("benchmarks.scorers.swebench.subprocess.run", fake_run)

    result = preflight_instance("container", _swebench_inst())

    assert result["status"] == "preflight_test_patch_apply_failed"
    assert result["category"] == "framework_preflight"
    assert "patch does not apply" in result["test_patch_apply_output"]


def test_preflight_fails_when_tracked_worktree_is_dirty(monkeypatch):
    calls = []

    def fake_run(cmd, **kwargs):
        calls.append(cmd)
        if cmd[:5] == ["docker", "exec", "container", "bash", "-c"] and "git diff --quiet HEAD -- ." in cmd[-1]:
            return _completed(returncode=1, stdout=" M tracked.py\n")
        return _completed(returncode=0)

    monkeypatch.setattr("benchmarks.scorers.swebench.subprocess.run", fake_run)

    result = preflight_instance("container", _swebench_inst())

    assert result["status"] == "preflight_dirty_worktree"
    assert result["category"] == "framework_preflight"
    assert "tracked.py" in result["worktree_status"]
    assert ["docker", "exec", "container", "rm", "-f", "/tmp/preflight_test_patch.diff"] not in calls


def test_score_reports_model_test_patch_conflict(monkeypatch):
    calls = []

    def fake_run(cmd, **kwargs):
        calls.append(cmd)
        if cmd[-2:] in (["tee", "/tmp/patch.diff"], ["tee", "/tmp/test_patch.diff"]):
            return _completed(returncode=0)
        if cmd[-2] == "-c" and "git apply --check /tmp/test_patch.diff" in cmd[-1]:
            return _completed(returncode=1, stderr="error: tests/test_example.py: patch does not apply")
        return _completed(returncode=0)

    monkeypatch.setattr("benchmarks.scorers.swebench.subprocess.run", fake_run)

    result = score_in_place("container", "diff --git a/a.py b/a.py\n", _swebench_inst())

    assert result["status"] == "model_test_patch_conflict"
    assert result["category"] == "evaluation_conflict"
    assert "patch does not apply" in result["test_patch_apply_output"]
    assert any("/tmp/patch.diff" in cmd[-1] for cmd in calls if cmd[-2] == "-c")


def _swebench_inst():
    return {
        "repo": "some/unknown",
        "version": "",
        "test_patch": (
            "diff --git a/tests/test_example.py b/tests/test_example.py\n"
            "--- a/tests/test_example.py\n"
            "+++ b/tests/test_example.py\n"
            "@@ -1 +1 @@\n-old\n+new\n"
        ),
        "FAIL_TO_PASS": '["tests/test_example.py::test_fix"]',
        "PASS_TO_PASS": '["tests/test_example.py::test_keep"]',
    }


def _completed(returncode=0, stdout="", stderr=""):
    import subprocess

    return subprocess.CompletedProcess([], returncode, stdout, stderr)
