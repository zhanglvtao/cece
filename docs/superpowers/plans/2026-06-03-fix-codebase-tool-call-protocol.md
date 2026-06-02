# Fix Codebase Tool Call Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop Codebase provider `invalid message` failures by serializing assistant tool-call history with the `function` field and by treating provider `invalid message` errors as non-recoverable.

**Architecture:** Keep Codebase stream decoding permissive because SSE chunks may contain either `function_call` or `function`, but make outbound request history use the OpenAI-compatible `tool_calls[].function` shape. Keep ordinary provider parameter errors recoverable while preventing `invalid message` from being converted into assistant text and appended back into history.

**Tech Stack:** Go, `internal/codebase`, `internal/agent`, Go testing.

---

## File Structure

- Modify: `internal/codebase/serialize.go`
  - Change outbound assistant tool-call serialization from `FunctionCall` to `Function`.
  - Update comments so request history and stream decoding behavior are not conflated.
- Modify: `internal/codebase/serialize_test.go`
  - Update direct serialization and JSON round-trip tests to require `function` and reject `function_call`.
- Modify: `internal/codebase/client_test.go`
  - Update request payload test for assistant messages containing thinking + tool use.
  - Assert replayed tool-only assistant messages include `Function` and not `FunctionCall`.
- Modify: `internal/agent/model_streamer.go`
  - Exclude Codebase `invalid message` protocol errors from recoverable provider errors.
- Create or modify: `internal/agent/model_streamer_test.go`
  - Add focused tests for Codebase invalid-message classification and retained recoverability for ordinary parameter errors.

### Task 1: Lock outbound Codebase tool-call shape with failing tests

**Files:**
- Modify: `internal/codebase/serialize_test.go`
- Modify: `internal/codebase/client_test.go`

- [ ] **Step 1: Update `TestSerializeAssistantWithTextAndToolUse` expectations**

Replace the tool-call assertions with:

```go
	if tc.Function == nil {
		t.Fatal("expected function to be non-nil")
	}
	if tc.FunctionCall != nil {
		t.Fatal("expected function_call to stay nil for serialized history")
	}
	if tc.Function.Name != "Bash" {
		t.Errorf("expected function.name 'Bash', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected arguments: %q", tc.Function.Arguments)
	}
```

- [ ] **Step 2: Update `TestSerializeJSONRoundTrip` expectations**

Replace the JSON key assertions with:

```go
	if _, ok := tc["function"]; !ok {
		t.Error("expected 'function' key in tool_call, not found")
	}
	if _, ok := tc["function_call"]; ok {
		t.Error("did not expect 'function_call' key in serialized history tool_call")
	}
```

- [ ] **Step 3: Update client request payload tests**

In `TestStreamStripsThinkingFromPayload`, read `toolCall["function"]` instead of `toolCall["function_call"]` and keep the same name/arguments assertions.

In `TestStreamSecondRequestReplayUsesEmptyContentArrayForToolOnlyAssistant`, add:

```go
	toolCall := assistant.ToolCalls[0]
	if toolCall.Function == nil {
		t.Fatal("assistant tool_call function is nil")
	}
	if toolCall.FunctionCall != nil {
		t.Fatal("assistant tool_call function_call should be nil in outbound history")
	}
	if toolCall.Function.Name != "Bash" {
		t.Fatalf("assistant tool_call function.name = %q, want Bash", toolCall.Function.Name)
	}
```

- [ ] **Step 4: Run the focused Codebase tests and confirm they fail**

Run:

```bash
go test ./internal/codebase -run 'TestSerializeAssistantWithTextAndToolUse|TestSerializeJSONRoundTrip|TestStreamStripsThinkingFromPayload|TestStreamSecondRequestReplayUsesEmptyContentArrayForToolOnlyAssistant' -count=1
```

Expected: FAIL because the current implementation still populates `FunctionCall`.

### Task 2: Serialize outbound history with `function`

**Files:**
- Modify: `internal/codebase/serialize.go`

- [ ] **Step 1: Update `CodebaseToolCall` comment**

Use this comment:

```go
// CodebaseToolCall represents a tool call in an assistant message. Outbound
// history uses OpenAI-style "function". Stream chunks may still use either
// "function_call" or "function", so both fields stay for decoding.
```

- [ ] **Step 2: Change `serializeMessage` to populate `Function`**

Change the tool-call construction to:

```go
					msg.ToolCalls = append(msg.ToolCalls, CodebaseToolCall{
						Index: len(msg.ToolCalls),
						ID:    cb.ToolUse.ID,
						Type:  "function",
						Function: &CodebaseFuncCall{
							Name:      cb.ToolUse.Name,
							Arguments: string(cb.ToolUse.Input),
						},
					})
```

- [ ] **Step 3: Run focused Codebase tests**

Run:

```bash
go test ./internal/codebase -run 'TestSerializeAssistantWithTextAndToolUse|TestSerializeJSONRoundTrip|TestStreamStripsThinkingFromPayload|TestStreamSecondRequestReplayUsesEmptyContentArrayForToolOnlyAssistant' -count=1
```

Expected: PASS.

### Task 3: Prevent `invalid message` recovery pollution

**Files:**
- Modify: `internal/agent/model_streamer.go`
- Create or modify: `internal/agent/model_streamer_test.go`

- [ ] **Step 1: Add classification tests**

Add tests in `internal/agent/model_streamer_test.go`:

```go
func TestIsRecoverableProviderError_CodebaseInvalidMessageIsNotRecoverable(t *testing.T) {
	err := errors.New("codebase api error: trae_permanent_error(invalid params): We're sorry, the param is invalid.; biz error: rpc error: code = ErrParamInvalid desc = invalid message, origin err = invalid message (code=4001)")

	if isRecoverableProviderError(err) {
		t.Fatal("expected codebase invalid message error to be non-recoverable")
	}
}

func TestIsRecoverableProviderError_CodebaseOrdinaryParamErrorIsRecoverable(t *testing.T) {
	err := errors.New("codebase api error: missing required field repo_name (code=4001)")

	if !isRecoverableProviderError(err) {
		t.Fatal("expected ordinary codebase parameter error to remain recoverable")
	}
}
```

- [ ] **Step 2: Run classification tests and confirm invalid-message test fails**

Run:

```bash
go test ./internal/agent -run 'TestIsRecoverableProviderError_Codebase' -count=1
```

Expected: FAIL for invalid-message classification before implementation.

- [ ] **Step 3: Update Codebase error classification**

Change the Codebase branch to:

```go
	if strings.Contains(msg, "codebase api error") {
		if strings.Contains(strings.ToLower(msg), "invalid message") {
			return false
		}
		if strings.Contains(msg, "code=4001") ||
			strings.Contains(msg, "ErrParamInvalid") ||
			strings.Contains(msg, "invalid param") ||
			strings.Contains(msg, "trae_permanent_error") {
			return true
		}
	}
```

- [ ] **Step 4: Run classification tests**

Run:

```bash
go test ./internal/agent -run 'TestIsRecoverableProviderError_Codebase' -count=1
```

Expected: PASS.

### Task 4: Run regression tests

**Files:**
- Test: `internal/codebase/...`
- Test: `internal/agent/...`

- [ ] **Step 1: Run package regressions**

Run:

```bash
go test ./internal/codebase/... ./internal/agent/...
```

Expected: PASS.

- [ ] **Step 2: Inspect diff for unrelated changes**

Run:

```bash
git diff -- internal/codebase/serialize.go internal/codebase/serialize_test.go internal/codebase/client_test.go internal/agent/model_streamer.go internal/agent/model_streamer_test.go docs/superpowers/plans/2026-06-03-fix-codebase-tool-call-protocol.md
```

Expected: diff only contains the protocol serialization fix, error classification tests/fix, and this plan.

## Self-Review

- Spec coverage: outbound Codebase request shape, stream decode compatibility, invalid-message non-recovery, and tests are all covered by tasks.
- Placeholder scan: no TBD/TODO placeholders remain.
- Type consistency: field names match existing `CodebaseToolCall.Function`, `FunctionCall`, and `CodebaseFuncCall` types.
