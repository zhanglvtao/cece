from benchmarks.scorers.swebench import _load_test_list
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
