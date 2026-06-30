# Agent Spawner Inbox Notification Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Agent tool communicate the intended spawn-return contract: after `operation=start`, the spawned agent returns pending/completed notifications to the spawning agent’s inbox, and the spawning agent should not proactively poll `status`/`wait` just to see whether the spawned agent finished.

**Architecture:** The runtime already has the right delivery path: the orchestrator receives terminal/pending messages from a spawned agent, writes result artifacts when needed, and appends a notification to the spawning engine’s `agentInbox`. `Engine.injectAgentNotifications` then injects unread inbox notifications into the spawning agent’s next model request. This change tightens model-visible wording, tests, and docs around that spawn-return contract without changing mailbox/event semantics.

**Tech Stack:** Go, existing `internal/tool` Agent tool schema, existing `internal/engine` orchestrator/inbox tests, markdown docs.

---

## Context

Current relevant flow:

- `internal/tool/agent_tool.go:101` describes Agent as async, but then says “Then use status/wait/send/answer/confirm/reject/cancel to drive it.” This wording encourages the spawning agent to poll immediately after `start`.
- `internal/engine/orchestrator.go:150` returns: “You will be notified when it completes; use Agent status or wait if you need to check sooner.” This also nudges polling.
- `internal/engine/engine.go:228` stores spawned-agent notifications in the spawning engine’s `agentInbox`.
- `internal/engine/engine.go:273` injects unread notifications into the spawning agent’s next model request as a synthetic user message.
- `internal/engine/orchestrator.go:458` appends completed spawned-agent results into the spawning engine’s inbox.
- Existing docs captured that UI events are insufficient and that mailbox semantics matter, but not the specific misuse: the spawning agent treating `Agent(start)` as a synchronous RPC and immediately calling `wait/status/answer` to chase the result.

The desired behavior is not to remove `status`/`wait`. Those operations remain useful when the user explicitly asks to inspect a spawned agent, when handling pending questions/confirmations, or when an explicit follow-up control action is required. The change is to stop presenting polling as the default next step after spawn.

## File Structure

Modify only these files:

- `internal/tool/agent_tool.go`
  - Responsibility: tool schema and model-facing description of Agent operations.
  - Change: rewrite `Definition.Description` and the `operation` description to state the spawn-return inbox contract.

- `internal/tool/agent_tool_test.go`
  - Responsibility: unit tests for Agent tool schema and formatting.
  - Change: update description test to require spawner/spawned wording and keep manual operations visible.

- `internal/engine/orchestrator.go`
  - Responsibility: Agent orchestration lifecycle and start response.
  - Change: rewrite `start` response content so it says the spawned agent will return notifications to the spawning agent’s inbox, not that the caller should use `status`/`wait` to check sooner.

- `internal/engine/orchestrator_test.go`
  - Responsibility: orchestrator lifecycle tests.
  - Change: extend `TestOrchestratorStartReturnsImmediately` to lock in the new start response.

- `internal/engine/engine.go`
  - Responsibility: injecting unread agent inbox notifications into model request context.
  - Change: rename the injected heading from “background workers” to “spawned agents”.

- `internal/engine/engine_test.go`
  - Responsibility: engine inbox injection tests.
  - Change: update assertions to match “spawned agents” and keep artifact-read guidance covered.

- `internal/engine/orchestrator_test.go`
  - Responsibility: orchestrator artifact/inbox tests.
  - Change: update any inbox-notification assertion that still expects “background workers”.

- `docs/development-issues.md`
  - Responsibility: project issue log.
  - Change: add a short issue record using `spawning agent`, `spawned agent`, and `spawning agent inbox` terminology.

No new files are required.

## Reuse

Reuse these existing mechanisms rather than introducing a new one:

- `Engine.agentInbox` and `appendAgentNotification` in `internal/engine/engine.go:228`.
- `Engine.injectAgentNotifications` in `internal/engine/engine.go:273`.
- `Orchestrator.handleTerminalMessage` in `internal/engine/orchestrator.go:448`.
- Existing `Agent` operations (`status`, `wait`, `send`, `answer`, `confirm`, `reject`, `cancel`, `switch_model`) remain available.

This plan intentionally avoids changing mailbox/event semantics. The runtime already routes spawned-agent output back to the spawning engine’s inbox; the model-visible contract is what needs correction.

## Implementation Tasks

### Task 1: Tighten Agent tool schema wording

**Files:**
- Modify: `internal/tool/agent_tool_test.go:102-110`
- Modify: `internal/tool/agent_tool.go:101-109`

- [ ] **Step 1: Update the failing test for the Agent description**

Replace `TestAgentToolDescriptionMentionsAsyncControlPlane` in `internal/tool/agent_tool_test.go` with:

```go
func TestAgentToolDescriptionMentionsAsyncControlPlane(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{})
	desc := agentTool.Info().Description
	for _, want := range []string{
		"independent subtasks",
		"parallelizable",
		"built-in",
		"research",
		"coding",
		"review",
		"execution",
		"asynchronously",
		"spawned agent",
		"spawning agent's inbox",
		"Do not proactively poll",
		"status",
		"wait",
		"send",
		"answer",
		"confirm",
		"reject",
		"cancel",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q: %q", want, desc)
		}
	}
}
```

- [ ] **Step 2: Run the focused failing test**

Run:

```bash
go test ./internal/tool -run TestAgentToolDescriptionMentionsAsyncControlPlane -count=1
```

Expected before implementation: FAIL because the current description does not contain `spawned agent`, `spawning agent's inbox`, or `Do not proactively poll`.

- [ ] **Step 3: Update the Agent tool description and operation schema**

In `internal/tool/agent_tool.go`, replace the `Description` field in `Info()` with:

```go
		Description: "Use Agent to spawn independent task agents for parallelizable work, long-running investigations, code changes, reviews, or execution that should continue outside the current turn. Agent includes built-in task-specific agents: research, coding, review, and execution. operation=start spawns one of these built-in agents asynchronously and requires agent_type to select which one to run. After start, the spawned agent returns pending/completed notifications to the spawning agent's inbox; Do not proactively poll status/wait just to see whether it finished. Use status/wait only when the user explicitly asks for a check, or when you need to inspect or drive a pending interaction. Use send/answer/confirm/reject/cancel/switch_model for explicit follow-up control. Multiple Agent start calls in a single response can run in parallel. Spawned agents have their own conversation history and tool set, share the project directory, and cannot spawn further agents.",
```

Replace the `operation` property description with:

```go
				"operation": map[string]any{
					"type":        "string",
					"description": "Operation: start (default), status, wait, send, answer, confirm, reject, switch_model, or cancel. After start, the spawned agent returns notifications to the spawning agent's inbox; use status/wait only for explicit checks or pending interaction handling.",
				},
```

- [ ] **Step 4: Run the focused passing test**

Run:

```bash
go test ./internal/tool -run TestAgentToolDescriptionMentionsAsyncControlPlane -count=1
```

Expected after implementation: PASS.

### Task 2: Change the start response away from polling-first wording

**Files:**
- Modify: `internal/engine/orchestrator_test.go:72-91`
- Modify: `internal/engine/orchestrator.go:146-151`

- [ ] **Step 1: Extend the orchestrator start test**

In `internal/engine/orchestrator_test.go`, inside `TestOrchestratorStartReturnsImmediately`, after the status assertion and before `close(block)`, add:

```go
	if !strings.Contains(result.Content, "spawning agent's inbox") {
		t.Fatalf("start content = %q, want spawning inbox guidance", result.Content)
	}
	if strings.Contains(result.Content, "status or wait") || strings.Contains(result.Content, "check sooner") {
		t.Fatalf("start content encourages polling: %q", result.Content)
	}
```

- [ ] **Step 2: Run the focused failing test**

Run:

```bash
go test ./internal/engine -run TestOrchestratorStartReturnsImmediately -count=1
```

Expected before implementation: FAIL because current content contains `status or wait` / `check sooner` and does not contain `spawning agent's inbox`.

- [ ] **Step 3: Update the orchestrator start response**

In `internal/engine/orchestrator.go`, replace the `Content` field in the `start` return value with:

```go
		Content:   fmt.Sprintf("Agent %s started asynchronously. The spawned agent will return pending/completed notifications to the spawning agent's inbox when it needs input or completes.", agentID),
```

The full return block should be:

```go
	return tool.AgentSubAgentResult{
		AgentID:   agentID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   fmt.Sprintf("Agent %s started asynchronously. The spawned agent will return pending/completed notifications to the spawning agent's inbox when it needs input or completes.", agentID),
	}, nil
```

- [ ] **Step 4: Run the focused passing test**

Run:

```bash
go test ./internal/engine -run TestOrchestratorStartReturnsImmediately -count=1
```

Expected after implementation: PASS.

### Task 3: Rename model-visible inbox notification heading to spawned agents

**Files:**
- Modify: `internal/engine/engine_test.go:110-142`
- Modify: `internal/engine/orchestrator_test.go:198-217`
- Modify: `internal/engine/engine.go:279`

- [ ] **Step 1: Update the engine inbox injection test**

In `internal/engine/engine_test.go`, inside `TestEngineInjectsUnreadAgentNotificationsIntoNextRequest`, replace the first `found` scan condition with:

```go
	for _, msg := range client.messages[0] {
		if strings.Contains(msg.Content, "Agent notifications from spawned agents") &&
			strings.Contains(msg.Content, "/tmp/result.txt") &&
			strings.Contains(msg.Content, "Use Read with this path to inspect the full result") {
			found = true
		}
	}
```

Also replace the second-pass duplicate check with:

```go
	for _, msg := range last {
		if strings.Contains(msg.Content, "Agent notifications from spawned agents") {
			t.Fatalf("notification injected twice: %+v", last)
		}
	}
```

- [ ] **Step 2: Update the orchestrator artifact/inbox test**

In `internal/engine/orchestrator_test.go`, inside `TestOrchestratorCompletedBackfillsArtifact`, replace the scan condition with:

```go
	for _, msg := range last {
		if strings.Contains(msg.Content, "Agent notifications from spawned agents") && strings.Contains(msg.Content, "Result artifact:") {
			found = true
		}
	}
```

- [ ] **Step 3: Run the focused failing tests**

Run:

```bash
go test ./internal/engine -run 'TestEngineInjectsUnreadAgentNotificationsIntoNextRequest|TestOrchestratorCompletedBackfillsArtifact' -count=1
```

Expected before implementation: FAIL because the implementation still emits `Agent notifications from background workers`.

- [ ] **Step 4: Update the notification heading implementation**

In `internal/engine/engine.go`, inside `injectAgentNotifications`, replace:

```go
	b.WriteString("Agent notifications from background workers:\n")
```

with:

```go
	b.WriteString("Agent notifications from spawned agents:\n")
```

- [ ] **Step 5: Run the focused passing tests**

Run:

```bash
go test ./internal/engine -run 'TestEngineInjectsUnreadAgentNotificationsIntoNextRequest|TestOrchestratorCompletedBackfillsArtifact' -count=1
```

Expected after implementation: PASS.

### Task 4: Record the development issue using spawn terminology

**Files:**
- Modify: `docs/development-issues.md:63-67`

- [ ] **Step 1: Add the issue entry**

Insert this entry after the existing “Agent 异步完成通知不能只走 UI event” section:

```markdown
## Agent start 后默认等待 spawner inbox，不主动轮询 status/wait
- 现象：使用 `Agent(operation=start)` spawn 出任务 agent 后，spawning agent 容易立刻调用 `wait/status/answer` 去追结果；如果工具层只返回完成状态而没有正文，就会制造“spawned agent 完成但结果丢了”的错觉。
- 定位：runtime 已经有正确的数据通道：spawned agent 的 terminal/pending message 会写入 spawning agent 的 `agentInbox`，并在下一次 spawning agent model request 前注入为模型可见通知。误导来自 Agent tool description 和 start 返回文案把 `status/wait` 表述成默认下一步。
- 结论：`start` 是 spawn 投递，不是同步 RPC。默认行为应是继续当前工作并等待 spawning agent inbox 回流；`status/wait` 只用于用户明确要求检查、或处理 pending interaction 的显式控制场景。
```

- [ ] **Step 2: Verify the note exists**

Run:

```bash
grep -n "Agent start 后默认等待 spawner inbox" docs/development-issues.md
```

Expected output includes one matching line.

### Task 5: Run targeted regression tests and inspect diff

**Files:**
- Verify only.

- [ ] **Step 1: Run focused tool and engine tests**

Run:

```bash
go test ./internal/tool ./internal/engine -count=1
```

Expected: PASS.

- [ ] **Step 2: Inspect the diff**

Run:

```bash
git diff -- internal/tool/agent_tool.go internal/tool/agent_tool_test.go internal/engine/orchestrator.go internal/engine/orchestrator_test.go internal/engine/engine.go internal/engine/engine_test.go docs/development-issues.md
```

Expected: diff only contains the wording/tests/docs changes described above. No runtime mailbox semantics should change.

- [ ] **Step 3: Commit the completed bugfix**

Run:

```bash
git add internal/tool/agent_tool.go internal/tool/agent_tool_test.go internal/engine/orchestrator.go internal/engine/orchestrator_test.go internal/engine/engine.go internal/engine/engine_test.go docs/development-issues.md
git commit -m "fix: clarify agent spawner inbox flow" -m "Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

Expected: commit succeeds after tests pass.

## Verification

Primary verification:

```bash
go test ./internal/tool ./internal/engine -count=1
```

Optional broader verification if time allows:

```bash
go test ./... -count=1
```

Manual check:

- Start response says the spawned agent returns notifications to the spawning agent’s inbox.
- Tool description says `Do not proactively poll`.
- Model-visible inbox heading says `Agent notifications from spawned agents`.
- `status` and `wait` remain documented for explicit checks and pending interaction handling.
- `docs/development-issues.md` records the spawn-return reasoning.

## Risks

- The phrase `Do not proactively poll` is intentionally strong. If it suppresses useful manual checks too much, soften only the surrounding sentence while keeping `status/wait only when explicit` intact.
- Tests assert English wording. This is acceptable here because the bug is a prompt/tool-description contract regression.
- This does not prevent a model from polling; it makes the intended contract clear. Hard enforcement would require more invasive runtime policy and is out of scope.
- Internal code still contains `parent`/`ParentSessionID` naming. This plan intentionally avoids schema/API renames; a deeper rename should be a separate migration plan.

## Non-goals

- Do not remove `status` or `wait` operations.
- Do not change mailbox channels, event delivery, or `agentInbox` data structures.
- Do not rename internal `ParentSessionID` / `ParentID` fields in this bugfix.
- Do not change UI rendering of running agents.
- Do not change artifact persistence or spawned-agent result truncation.
- Do not add a new subagent API.

## Self-review

- Spec coverage: covers the confirmed requirement that a spawned agent should return results to the spawning agent’s inbox, and that the spawning agent should not proactively query status after `start`.
- Placeholder scan: no TBD/TODO placeholders remain.
- Type consistency: all referenced functions and files already exist; no new types are introduced.
- Scope check: single small bugfix focused on model-visible Agent control-plane wording plus regression tests/docs.
