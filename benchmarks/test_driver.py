from benchmarks.driver import CeceDriver


class FakeProc:
    stdin = None
    stdout = None
    stderr = None

    def poll(self):
        return None


class ScriptedDriver(CeceDriver):
    def __init__(self, events):
        super().__init__(FakeProc())
        self._events = events
        self.sent = []

    def send(self, action):
        self.sent.append(action)

    def send_input(self, text):
        self.sent.append({"type": "action", "kind": "input", "payload": {"text": text}})

    def events_with_poll(self, poll_interval=5.0):
        yield from self._events

    def events(self):
        return iter(())


def event(kind, payload=None):
    return {"type": "event", "kind": kind, "payload": payload or {}}


def test_run_until_done_completes_on_assistant_completed_without_turn_completed():
    driver = ScriptedDriver([
        event("completion_gate_evaluated", {"Status": "passed", "Next": "complete"}),
        event("assistant_completed", {"Duration": 123}),
    ])

    result = driver.run_until_done("fix issue", timeout=60)

    assert result.exit_status == "completed"
    assert [ev["kind"] for ev in result.transcript] == [
        "completion_gate_evaluated",
        "assistant_completed",
    ]


def test_run_until_done_still_completes_on_turn_completed():
    driver = ScriptedDriver([
        event("assistant_completed", {"Duration": 123}),
        event("turn_completed", {"TurnCount": 1}),
    ])

    result = driver.run_until_done("fix issue", timeout=60)

    assert result.exit_status == "completed"
    assert result.transcript[-1]["kind"] in {"assistant_completed", "turn_completed"}
