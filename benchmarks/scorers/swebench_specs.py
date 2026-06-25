"""Official SWE-bench per-repo test specs and log parsers (self-contained).

This module embeds the parts of the official `swebench` package that we need
for scoring, because that package requires Python 3.10+ (PEP 604 syntax) and
is unusable on the Python 3.9 host. We replicate only:

  * The per-repo (and where needed per-version) test command (`test_cmd`).
  * The per-repo pytest log parser that maps each test id -> PASSED/FAILED/etc.

Resolution is then computed the official way: all FAIL_TO_PASS tests must pass
AND all PASS_TO_PASS tests must still pass.

Source of truth (mirrored): SWE-bench/swebench/harness/constants/python.py and
SWE-bench/swebench/harness/log_parsers/python.py.
"""

import re

# --- Test status markers (official TestStatus enum values) -------------------
PASSED = "PASSED"
FAILED = "FAILED"
ERROR = "ERROR"
SKIPPED = "SKIPPED"
XFAIL = "XFAIL"

_STATUS_PREFIXES = (PASSED, FAILED, ERROR, SKIPPED, XFAIL)

# --- Test commands (official) ------------------------------------------------
TEST_PYTEST = "pytest -rA --tb=long -p no:cacheprovider"
TEST_ASTROPY_PYTEST = "pytest -rA -vv -o console_output_style=classic --tb=no"
TEST_DJANGO = "./tests/runtests.py --verbosity 2 --settings=test_sqlite --parallel 1"
TEST_DJANGO_NO_PARALLEL = "./tests/runtests.py --verbosity 2"
TEST_SEABORN = "pytest --no-header -rA"
TEST_SYMPY = (
    "PYTHONWARNINGS='ignore::UserWarning,ignore::SyntaxWarning' bin/test -C --verbose"
)

# Per-repo default test command. Most pytest repos share TEST_PYTEST.
# astropy uses a classic-style pytest invocation; django/sympy/seaborn differ.
MAP_REPO_TO_TEST_CMD = {
    "astropy/astropy": TEST_ASTROPY_PYTEST,
    "django/django": TEST_DJANGO,
    "matplotlib/matplotlib": TEST_PYTEST,
    "marshmallow-code/marshmallow": TEST_PYTEST,
    "mwaskom/seaborn": TEST_SEABORN,
    "pallets/flask": TEST_PYTEST,
    "psf/requests": TEST_PYTEST,
    "pvlib/pvlib-python": TEST_PYTEST,
    "pydata/xarray": TEST_PYTEST,
    "pydicom/pydicom": TEST_PYTEST,
    "pylint-dev/astroid": TEST_PYTEST,
    "pylint-dev/pylint": TEST_PYTEST,
    "pytest-dev/pytest": TEST_PYTEST,
    "pyvista/pyvista": TEST_PYTEST,
    "scikit-learn/scikit-learn": TEST_PYTEST,
    "sphinx-doc/sphinx": "tox --current-env -epy39 -v --",
    "sqlfluff/sqlfluff": TEST_PYTEST,
    "sympy/sympy": TEST_SYMPY,
}

# A handful of django versions disable parallelism.
MAP_REPO_VERSION_TO_TEST_CMD = {
    ("django/django", "1.9"): TEST_DJANGO_NO_PARALLEL,
}

# Non-test file extensions to strip from test_patch directives.
NON_TEST_EXTS = [
    ".json", ".png", "csv", ".txt", ".md", ".jpg", ".jpeg", ".pkl",
    ".yml", ".yaml", ".toml", ".rst",
]


def get_test_cmd(repo: str, version: str) -> str:
    """Return the official base test command for repo@version."""
    key = (repo, version)
    if key in MAP_REPO_VERSION_TO_TEST_CMD:
        return MAP_REPO_VERSION_TO_TEST_CMD[key]
    return MAP_REPO_TO_TEST_CMD.get(repo, TEST_PYTEST)


def get_test_directives(repo: str, test_patch: str) -> list[str]:
    """Extract test files/modules from the test_patch (official logic)."""
    if not test_patch:
        return []
    diff_pat = r"diff --git a/.* b/(.*)"
    directives = re.findall(diff_pat, test_patch)
    directives = [
        d for d in directives if not any(d.endswith(ext) for ext in NON_TEST_EXTS)
    ]
    if repo == "django/django":
        transformed = []
        for d in directives:
            d = d[: -len(".py")] if d.endswith(".py") else d
            d = d[len("tests/"):] if d.startswith("tests/") else d
            d = d.replace("/", ".")
            transformed.append(d)
        directives = transformed
    return directives


# --- Log parsers (official) --------------------------------------------------
def _parse_log_pytest(log: str) -> dict[str, str]:
    """Standard pytest -rA parser: lines like 'PASSED path::test'."""
    test_status_map = {}
    for line in log.split("\n"):
        if any(line.startswith(p) for p in _STATUS_PREFIXES):
            if line.startswith(FAILED):
                line = line.replace(" - ", " ")
            parts = line.split()
            if len(parts) <= 1:
                continue
            test_status_map[parts[1]] = parts[0]
    return test_status_map


def _parse_log_pytest_v2(log: str) -> dict[str, str]:
    """Later-pytest parser with ANSI escape stripping (astropy/sklearn/sphinx)."""
    test_status_map = {}
    escapes = "".join(chr(c) for c in range(1, 32))
    translator = str.maketrans("", "", escapes)
    for line in log.split("\n"):
        line = re.sub(r"\[(\d+)m", "", line)
        line = line.translate(translator)
        if any(line.startswith(p) for p in _STATUS_PREFIXES):
            if line.startswith(FAILED):
                line = line.replace(" - ", " ")
            parts = line.split()
            if len(parts) >= 2:
                test_status_map[parts[1]] = parts[0]
        elif any(line.endswith(p) for p in _STATUS_PREFIXES):
            parts = line.split()
            if len(parts) >= 2:
                test_status_map[parts[0]] = parts[1]
    return test_status_map


def _parse_log_pytest_options(log: str) -> dict[str, str]:
    """pytest parser that normalizes parametrize options (requests/pydicom/pylint)."""
    option_pattern = re.compile(r"(.*?)\[(.*)\]")
    test_status_map = {}
    for line in log.split("\n"):
        if any(line.startswith(p) for p in _STATUS_PREFIXES):
            if line.startswith(FAILED):
                line = line.replace(" - ", " ")
            parts = line.split()
            if len(parts) <= 1:
                continue
            has_option = option_pattern.search(parts[1])
            if has_option:
                main, option = has_option.groups()
                if option.startswith("/") and not option.startswith("//") and "*" not in option:
                    option = "/" + option.split("/")[-1]
                test_name = f"{main}[{option}]"
            else:
                test_name = parts[1]
            test_status_map[test_name] = parts[0]
    return test_status_map


def _parse_log_django(log: str) -> dict[str, str]:
    """Django test runner parser."""
    test_status_map = {}
    prev_test = None
    for line in log.split("\n"):
        line = line.strip()
        if " ... " in line:
            prev_test = line.split(" ... ")[0]
        for suffix in (" ... ok", " ... OK", " ...  OK"):
            if line.endswith(suffix):
                test = line.rsplit(suffix, 1)[0]
                test_status_map[test] = PASSED
                break
        if " ... skipped" in line:
            test_status_map[line.split(" ... skipped")[0]] = SKIPPED
        if line.endswith(" ... FAIL"):
            test_status_map[line.split(" ... FAIL")[0]] = FAILED
        if line.startswith("FAIL:"):
            test_status_map[line.split()[1].strip()] = FAILED
        if line.endswith(" ... ERROR"):
            test_status_map[line.split(" ... ERROR")[0]] = ERROR
        if line.startswith("ERROR:"):
            test_status_map[line.split()[1].strip()] = ERROR
        if line.lstrip().startswith("ok") and prev_test is not None:
            test_status_map[prev_test] = PASSED
    return test_status_map


def _parse_log_sympy(log: str) -> dict[str, str]:
    test_status_map = {}
    pattern = r"(_*) (.*)\.py:(.*) (_*)"
    for match in re.findall(pattern, log):
        test_status_map[f"{match[1]}.py:{match[2]}"] = FAILED
    for line in log.split("\n"):
        line = line.strip()
        if line.startswith("test_"):
            if line.endswith(" E"):
                test_status_map[line.split()[0]] = ERROR
            if line.endswith(" F"):
                test_status_map[line.split()[0]] = FAILED
            if line.endswith(" ok"):
                test_status_map[line.split()[0]] = PASSED
    return test_status_map


def _parse_log_seaborn(log: str) -> dict[str, str]:
    test_status_map = {}
    for line in log.split("\n"):
        if line.startswith(FAILED):
            test_status_map[line.split()[1]] = FAILED
        elif f" {PASSED} " in line:
            parts = line.split()
            if parts[1] == PASSED:
                test_status_map[parts[0]] = PASSED
        elif line.startswith(PASSED):
            test_status_map[line.split()[1]] = PASSED
    return test_status_map


MAP_REPO_TO_PARSER = {
    "astropy/astropy": _parse_log_pytest_v2,
    "django/django": _parse_log_django,
    "marshmallow-code/marshmallow": _parse_log_pytest,
    "matplotlib/matplotlib": _parse_log_pytest,
    "mwaskom/seaborn": _parse_log_seaborn,
    "pallets/flask": _parse_log_pytest,
    "psf/requests": _parse_log_pytest_options,
    "pvlib/pvlib-python": _parse_log_pytest,
    "pydata/xarray": _parse_log_pytest,
    "pydicom/pydicom": _parse_log_pytest_options,
    "pylint-dev/astroid": _parse_log_pytest,
    "pylint-dev/pylint": _parse_log_pytest_options,
    "pytest-dev/pytest": _parse_log_pytest,
    "pyvista/pyvista": _parse_log_pytest,
    "scikit-learn/scikit-learn": _parse_log_pytest_v2,
    "sphinx-doc/sphinx": _parse_log_pytest_v2,
    "sqlfluff/sqlfluff": _parse_log_pytest,
    "sympy/sympy": _parse_log_sympy,
}


def parse_test_log(repo: str, log: str) -> dict[str, str]:
    """Parse raw test output into {test_id: STATUS} using the repo's parser."""
    parser = MAP_REPO_TO_PARSER.get(repo, _parse_log_pytest)
    return parser(log)


def compute_resolution(test_status_map: dict[str, str],
                       fail_to_pass: list[str],
                       pass_to_pass: list[str]) -> dict:
    """Official resolution: all FAIL_TO_PASS pass AND all PASS_TO_PASS pass."""
    def passed(test_id: str) -> bool:
        return test_status_map.get(test_id) == PASSED

    f2p_passed = [t for t in fail_to_pass if passed(t)]
    f2p_failed = [t for t in fail_to_pass if not passed(t)]
    p2p_passed = [t for t in pass_to_pass if passed(t)]
    p2p_failed = [t for t in pass_to_pass if not passed(t)]

    resolved = len(f2p_failed) == 0 and len(p2p_failed) == 0
    # If PASS_TO_PASS is empty (e.g. SWE-bench_Lite sometimes omits), only F2P matters.
    if not pass_to_pass:
        resolved = len(f2p_failed) == 0 and len(f2p_passed) > 0

    return {
        "resolved": resolved,
        "fail_to_pass_passed": f2p_passed,
        "fail_to_pass_failed": f2p_failed,
        "pass_to_pass_passed_count": len(p2p_passed),
        "pass_to_pass_failed": p2p_failed,
    }
