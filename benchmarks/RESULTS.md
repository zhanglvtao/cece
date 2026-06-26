# SWE-bench Results Log

Tracking of SWE-bench_Lite runs: per-case outcome, model, time, and failure cause.

## Run: 2026-06-26 — SWE-bench_Lite `:10` — `traecli/GPT-5.5`

Scoring uses the official methodology (per-repo `test_cmd` + official log
parser; resolved = all FAIL_TO_PASS pass AND all PASS_TO_PASS still pass).

**Summary: Resolved 3 / 10 (30%)** — 3 PASS, 5 genuine FAIL, 2 timeout.

| # | Instance | Model | Result | Time(s) | F2P (pass/total) | P2P (fail/total) | Failure cause |
|---|----------|-------|--------|---------|------------------|------------------|---------------|
| 1 | astropy__astropy-12907 | traecli/GPT-5.5 | PASS (resolved) | 192.6 | 2/2 | 0/13 | — |
| 2 | astropy__astropy-14182 | traecli/GPT-5.5 | FAIL | 134.2 | 0/1 | 0/9 | Incomplete fix: only added `header_rows` param to `RST.__init__`, never updated `write()`. Header rows parsed as data → `ValueError: could not convert string to float: 'float64'`. |
| 3 | astropy__astropy-14365 | traecli/GPT-5.5 | FAIL | 142.9 | 0/1 | 0/8 | Wrong approach: added `re.IGNORECASE` only; dropped the expected warning path → `Failed: DID NOT WARN (AstropyUserWarning)`. |
| 4 | astropy__astropy-14995 | traecli/GPT-5.5 | PASS (resolved) | 184.0 | 1/1 | 0/179 | — |
| 5 | astropy__astropy-6938 | traecli/GPT-5.5 | PASS (resolved) | 166.2 | 2/2 | 0/11 | — (was the originally-broken case; now fixed) |
| 6 | astropy__astropy-7746 | traecli/GPT-5.5 | FAIL | 145.3 | 0/1 | 0/56 | Incomplete fix: added a zero-size early-return in only one entry point; other code paths still crash on empty arrays → `test_zero_size_input` fails. |
| 7 | django__django-10914 | traecli/GPT-5.5 | FAIL | 132.5 | 0/1 | 13/98 | Regression: hard-coded `FILE_UPLOAD_PERMISSIONS = 0o644` in global_settings; broke 13 PASS_TO_PASS OverrideSettings tests. Correct fix is in FileSystemStorage. (P2P check correctly caught the regression.) |
| 8 | django__django-10924 | traecli/GPT-5.5 | FAIL | 174.2 | 0/1 | 0/1 | Wrong file: patched forms-layer `FilePathField`, but target test is the models-layer field. `formfield().path` still returns the uncalled callable → assertion fails. |
| 9 | django__django-11001 | traecli/GPT-5.5 | TIMEOUT | 618.5 | — | — | Exceeded 600s timeout (model still working). |
| 10 | django__django-11019 | traecli/GPT-5.5 | TIMEOUT | 617.1 | — | — | Exceeded 600s timeout (model still working). |

### Notes
- All finished cases received real official-methodology scoring (3–180 tests
  parsed per case). No infra-induced `run_failed` / `apply_failed` / blank-parse.
- The 5 FAILs are legitimate model misses (incomplete / wrong-location / wrong-
  approach / regression), not framework bugs.
- `django-10914` demonstrates the value of the official PASS_TO_PASS check: a
  patch that "fixes the bug" but breaks 13 other tests is correctly failed.
- Timeouts (django-11001/11019) are the only non-evaluative failures; raising
  `--timeout` for django would let them complete.

### Timing note
GPT-5.5 is markedly faster than the earlier deepseek-v4-pro runs (e.g.
astropy-12907: 192s vs 453s). The speedup is from lower per-request model
latency (40 model requests + 36 tool calls finished in 192s), plus early-done
signalling letting the agent stop once the fix is confirmed instead of waiting
for the timeout.
