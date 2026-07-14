from types import SimpleNamespace

import pytest

from benchmarks import cli
from benchmarks.result import RunRecord


class FakeStore:
    def __init__(self, path=None):
        self.records = []
        self._dir = path

    def save_run(self, record):
        self.records.append(record)


class FakeSWEBenchAdapter:
    name = "swebench"

    def __init__(self):
        self.cleaned = False

    def instance_id(self, inst):
        return inst["instance_id"]

    def setup_sandbox(self, inst, cece_bin, config):
        return SimpleNamespace(
            exec_cmd=["cece"],
            env={},
            extra={"container_name": "container", "instance": inst},
        )

    def teardown_sandbox(self, sandbox):
        self.cleaned = True


def test_run_one_skips_engine_when_swebench_preflight_fails(monkeypatch):
    def fake_preflight(container_name, inst, log=None):
        return {
            "status": "preflight_test_patch_apply_failed",
            "category": "framework_preflight",
            "resolved": False,
        }

    monkeypatch.setattr("benchmarks.scorers.swebench.preflight_instance", fake_preflight)
    monkeypatch.setattr(
        cli.CeceDriver,
        "start",
        staticmethod(lambda *args, **kwargs: pytest.fail("CeceDriver.start should not be called")),
    )

    adapter = FakeSWEBenchAdapter()
    store = FakeStore()

    cli._run_one(
        adapter,
        {"instance_id": "django__django-10914"},
        "cece",
        {"model": "traecli/GPT-5.5"},
        60,
        store,
    )

    assert len(store.records) == 1
    assert store.records[0].exit_status == "skipped"
    assert store.records[0].artifact["score"]["category"] == "framework_preflight"
    assert adapter.cleaned is True


def test_write_predictions_excludes_preflight_skipped_records(tmp_path):
    store = FakeStore(tmp_path)
    records = [
        RunRecord(
            benchmark="swebench",
            instance_id="django__django-10914",
            model="traecli/GPT-5.5",
            exit_status="skipped",
            artifact={
                "patch": "diff --git a/a.py b/a.py\n",
                "score": {"status": "preflight_test_patch_apply_failed"},
            },
        )
    ]

    cli._write_predictions(store, records, None, "swebench")

    assert not (tmp_path / "predictions.jsonl").exists()


def test_print_summary_counts_preflight_and_eval_conflict(capsys):
    records = [
        RunRecord(
            benchmark="swebench",
            instance_id="preflight",
            model="m",
            exit_status="skipped",
            artifact={"score": {"category": "framework_preflight", "resolved": False}},
        ),
        RunRecord(
            benchmark="swebench",
            instance_id="conflict",
            model="m",
            exit_status="completed",
            artifact={"score": {"category": "evaluation_conflict", "resolved": False}},
        ),
        RunRecord(
            benchmark="swebench",
            instance_id="score-error",
            model="m",
            exit_status="completed",
            artifact={"score": {"status": "score_apply_failed", "category": "scoring_error", "resolved": False}},
        ),
        RunRecord(
            benchmark="swebench",
            instance_id="resolved",
            model="m",
            exit_status="completed",
            artifact={"score": {"status": "resolved", "resolved": True}},
        ),
    ]

    cli._print_summary(records)

    output = capsys.readouterr().out
    assert "Done: 3" in output
    assert "Skipped: 1" in output
    assert "Resolved: 1" in output
    assert "PreflightFailed: 1" in output
    assert "EvalConflict: 1" in output
    assert "ScoreError: 1" in output
