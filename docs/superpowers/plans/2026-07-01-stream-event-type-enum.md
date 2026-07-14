# StreamEventType Enum Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the bare `string` `EventType` field on stream events with a named `StreamEventType` type plus a canonical set of constants, so SSE event-type comparisons and producers are compiler-checked instead of relying on magic string literals.

**Architecture:** Go has no true enums; the codebase already uses the "named string type + `const` block" convention (`agent.ApiContentBlockType` ↔ `protocol.ContentBlockType`, converted at the adapter boundary in `internal/agent/adapter.go:241`). We follow that exact pattern: define `agent.StreamEventType` for the internal `ApiStreamEvent`/`agent.StreamEventDetail`, mirror it as `protocol.StreamEventType` for the wire-facing `protocol.StreamEventDetail`, and convert between them in the adapter. Because Go untyped string constants auto-convert to a named string type, producer literals and `==` comparisons keep compiling during the migration; the only hard breaks are the few places that assign the field into a plain `string` (cassette DTO, `compactJoin`, test vars) or across the two named types (adapter). Those are fixed in one atomic commit, then literals are swapped for constants package-by-package.

**Tech Stack:** Go 1.x, standard `testing`, `go build`, `go test`.

**Scope note — `Detail` is out of scope.** The sibling `Detail` field (`text_delta`, `input_json_delta`, `thinking_delta`, `stop_reason`, …) is also a bare string but its value set is more provider-specific and cross-cutting. It is intentionally NOT touched in this plan and remains `string`.

---

## Canonical constant mapping (used by every task)

Wire value → constant name (identical spelling in both `agent` and `protocol` packages):

| Wire string           | Constant                 |
|-----------------------|--------------------------|
| `"message_start"`     | `EventMessageStart`      |
| `"message_delta"`     | `EventMessageDelta`      |
| `"message_stop"`      | `EventMessageStop`       |
| `"content_block_start"` | `EventContentBlockStart` |
| `"content_block_delta"` | `EventContentBlockDelta` |
| `"content_block_stop"`  | `EventContentBlockStop`  |

Verified via `grep` that no identifier named `StreamEventType`, `EventMessageStart`, `EventMessageDelta`, `EventMessageStop`, `EventContentBlockStart`, `EventContentBlockDelta`, or `EventContentBlockStop` currently exists anywhere in `internal/` — no collisions.

Note: `"message_stop"` is emitted by producers as `agent.ApiStreamEvent{Done: true}` (no `EventType`) in the Claude path, but the aiden/testkit/eval paths DO set `EventType: "message_stop"`. Both are covered by including `EventMessageStop`.

---

## Task 1: Define the `StreamEventType` types and constants

Add the named types + constant blocks. Nothing is retyped yet, so the whole tree keeps compiling (unused constants are legal in Go). A characterization test locks the wire values.

**Files:**
- Modify: `internal/agent/message.go` (add type + consts near the existing `ApiContentBlockType` block, around line 47-55)
- Modify: `internal/protocol/types.go` (add mirror type + consts near the existing `ContentBlockType` block, around line 63-70)
- Test: `internal/agent/stream_event_type_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/agent/stream_event_type_test.go`:

```go
package agent

import "testing"

func TestStreamEventTypeWireValues(t *testing.T) {
	cases := []struct {
		got  StreamEventType
		want string
	}{
		{EventMessageStart, "message_start"},
		{EventMessageDelta, "message_delta"},
		{EventMessageStop, "message_stop"},
		{EventContentBlockStart, "content_block_start"},
		{EventContentBlockDelta, "content_block_delta"},
		{EventContentBlockStop, "content_block_stop"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("StreamEventType = %q, want %q", string(c.got), c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestStreamEventTypeWireValues`
Expected: FAIL — compile error `undefined: StreamEventType` (and undefined constants).

- [ ] **Step 3: Add the type + constants in `agent`**

In `internal/agent/message.go`, immediately after the `ApiContentBlockType` const block (after line 55, before `type ApiContentBlock struct`), insert:

```go
// StreamEventType identifies the kind of a streamed SSE event as normalized
// across providers (Anthropic-style event names). See ApiStreamEvent.EventType.
type StreamEventType string

const (
	EventMessageStart      StreamEventType = "message_start"
	EventMessageDelta      StreamEventType = "message_delta"
	EventMessageStop       StreamEventType = "message_stop"
	EventContentBlockStart StreamEventType = "content_block_start"
	EventContentBlockDelta StreamEventType = "content_block_delta"
	EventContentBlockStop  StreamEventType = "content_block_stop"
)
```

- [ ] **Step 4: Add the mirror type + constants in `protocol`**

In `internal/protocol/types.go`, immediately after the `ContentBlockType` const block (after line 70, before `// ContentBlock is a discriminated union`), insert:

```go
// StreamEventType identifies the kind of a streamed SSE event. Mirror of
// agent.StreamEventType for the wire-facing protocol layer.
type StreamEventType string

const (
	EventMessageStart      StreamEventType = "message_start"
	EventMessageDelta      StreamEventType = "message_delta"
	EventMessageStop       StreamEventType = "message_stop"
	EventContentBlockStart StreamEventType = "content_block_start"
	EventContentBlockDelta StreamEventType = "content_block_delta"
	EventContentBlockStop  StreamEventType = "content_block_stop"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestStreamEventTypeWireValues`
Expected: PASS

- [ ] **Step 6: Verify the whole tree still builds**

Run: `go build ./...`
Expected: no output (success) — no field types changed yet.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/message.go internal/protocol/types.go internal/agent/stream_event_type_test.go
git commit -m "feat: add StreamEventType enum type and constants"
```

---

## Task 2: Retype the `EventType` fields and fix conversion boundaries

Change the three struct fields to the new named types and repair every site that assigns the field into a plain `string` or across the two named types. This is one atomic commit that must keep `go build ./...` green.

**Files:**
- Modify: `internal/agent/message.go:102` (`ApiStreamEvent.EventType`)
- Modify: `internal/agent/events.go:103` (`agent.StreamEventDetail.EventType`)
- Modify: `internal/protocol/event.go:141` (`protocol.StreamEventDetail.EventType`)
- Modify: `internal/agent/adapter.go:67` (convert `agent` → `protocol`)
- Modify: `internal/evals/recording/cassette.go:27,48,79` (DTO mirror uses plain `string`)
- Modify: `internal/observatory/store.go:556,558` (`compactJoin` takes `...string`)
- Modify: `internal/claude/stream_test.go:74,136` (string-typed test vars)

- [ ] **Step 1: Retype the three struct fields**

In `internal/agent/message.go`, change line 102 from:

```go
	EventType           string // "message_start", "content_block_delta", etc.
```
to:
```go
	EventType           StreamEventType // "message_start", "content_block_delta", etc.
```

In `internal/agent/events.go`, change line 103 from:

```go
	EventType string // "content_block_delta", "message_delta", etc.
```
to:
```go
	EventType StreamEventType // "content_block_delta", "message_delta", etc.
```

In `internal/protocol/event.go`, change line 141 from:

```go
	EventType string // "content_block_delta", "message_delta", etc.
```
to:
```go
	EventType StreamEventType // "content_block_delta", "message_delta", etc.
```

- [ ] **Step 2: Run build to see the boundary breaks**

Run: `go build ./...`
Expected: FAIL — mismatched-type errors at `internal/agent/adapter.go`, `internal/evals/recording/cassette.go`, `internal/observatory/store.go`, and `internal/claude/stream_test.go` (the sites fixed in Steps 3-6). Producer literals and `==` comparisons will NOT error (untyped constants auto-convert).

- [ ] **Step 3: Fix the adapter cross-type conversion**

In `internal/agent/adapter.go`, the `StreamEventDetail` case (lines 65-70) currently reads:

```go
	case StreamEventDetail:
		return protocol.StreamEventDetail{
			EventType: v.EventType,
			Detail:    v.Detail,
			Text:      v.Text,
		}
```
Change the `EventType` line to convert between the two named types:
```go
	case StreamEventDetail:
		return protocol.StreamEventDetail{
			EventType: protocol.StreamEventType(v.EventType),
			Detail:    v.Detail,
			Text:      v.Text,
		}
```

- [ ] **Step 4: Fix the cassette DTO mirror**

The cassette DTO deliberately keeps `string` fields for stable JSON. In `internal/evals/recording/cassette.go`:

Line 27 stays `string` (it is the JSON DTO field — do NOT retype it):
```go
	EventType          string `json:"event_type,omitempty"`
```

In `fromApiEvent` (line 48), change:
```go
		EventType:          e.EventType,
```
to:
```go
		EventType:          string(e.EventType),
```

In `ToApiEvent` (line 79), change:
```go
		EventType:          ce.EventType,
```
to:
```go
		EventType:          agent.StreamEventType(ce.EventType),
```

- [ ] **Step 5: Fix `compactJoin` calls in the observatory store**

In `internal/observatory/store.go`, the `protocol.StreamEventDetail` case (lines 555-558) reads:

```go
		if e.Text != "" {
			return "stream " + compactJoin(e.EventType, e.Detail, firstLine(e.Text))
		}
		return "stream " + compactJoin(e.EventType, e.Detail)
```
`compactJoin(parts ...string)` needs plain strings, so convert `e.EventType`:
```go
		if e.Text != "" {
			return "stream " + compactJoin(string(e.EventType), e.Detail, firstLine(e.Text))
		}
		return "stream " + compactJoin(string(e.EventType), e.Detail)
```
(The comparisons on lines 552 — `e.EventType == "content_block_delta"` etc. — stay unchanged; they still compile.)

- [ ] **Step 6: Fix the string-typed test vars in claude/stream_test.go**

In `internal/claude/stream_test.go`, `var gotEventType string` is assigned from `chunk.EventType`. Convert at the assignment so the `string` var and its literal comparison keep working.

Line 74 (inside `TestParseStreamEmits...InputTokens` around line 72-75), change:
```go
			gotEventType = chunk.EventType
```
to:
```go
			gotEventType = string(chunk.EventType)
```

Line 136 (inside the delta test around line 134-138), change:
```go
			gotEventType = chunk.EventType
```
to:
```go
			gotEventType = string(chunk.EventType)
```

- [ ] **Step 7: Verify the whole tree builds**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 8: Run the full affected test set**

Run: `go test ./internal/agent/... ./internal/protocol/... ./internal/claude/... ./internal/aiden/... ./internal/observatory/... ./internal/evals/... ./internal/ipc/... ./internal/engine/... ./internal/testkit/...`
Expected: PASS (all packages ok).

- [ ] **Step 9: Commit**

```bash
git add internal/agent/message.go internal/agent/events.go internal/protocol/event.go internal/agent/adapter.go internal/evals/recording/cassette.go internal/observatory/store.go internal/claude/stream_test.go
git commit -m "refactor: retype stream EventType fields to StreamEventType"
```

---

## Task 3: Use constants in `internal/agent/model_streamer.go` (consumer)

Swap the magic-string comparisons for the typed constants. Each edit compiles independently because the field is now `StreamEventType`.

**Files:**
- Modify: `internal/agent/model_streamer.go` (lines 175, 198, 225, 249, 283, 289, 296, 311)

- [ ] **Step 1: Replace each comparison literal with its constant**

Apply these exact replacements in `internal/agent/model_streamer.go` (the constants are in the same `agent` package, so no qualifier):

- Line 175: `if chunk.EventType == "message_start" {` → `if chunk.EventType == EventMessageStart {`
- Line 198: `if chunk.EventType == "message_delta" {` → `if chunk.EventType == EventMessageDelta {`
- Line 225: `if chunk.EventType == "content_block_start" && chunk.ToolCallID != "" {` → `if chunk.EventType == EventContentBlockStart && chunk.ToolCallID != "" {`
- Line 249: `if chunk.EventType == "content_block_stop" {` → `if chunk.EventType == EventContentBlockStop {`
- Line 283: `if chunk.EventType == "content_block_start" && chunk.IsThinking {` → `if chunk.EventType == EventContentBlockStart && chunk.IsThinking {`
- Line 289: `if chunk.EventType == "content_block_start" && chunk.IsRedactedThinking {` → `if chunk.EventType == EventContentBlockStart && chunk.IsRedactedThinking {`
- Line 296: `if chunk.EventType == "content_block_stop" && thinkingIndex >= 0 && chunk.Index == thinkingIndex {` → `if chunk.EventType == EventContentBlockStop && thinkingIndex >= 0 && chunk.Index == thinkingIndex {`
- Line 311: `if chunk.EventType == "content_block_stop" && redactedThinkingIndex >= 0 && chunk.Index == redactedThinkingIndex {` → `if chunk.EventType == EventContentBlockStop && redactedThinkingIndex >= 0 && chunk.Index == redactedThinkingIndex {`

Leave the `chunk.EventType != ""` guard on line 166 and all `chunk.Detail == "..."` checks unchanged (Detail is out of scope; empty-string comparison is fine against a named string type).

- [ ] **Step 2: Verify build**

Run: `go build ./internal/agent/`
Expected: no output (success).

- [ ] **Step 3: Run agent tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/model_streamer.go
git commit -m "refactor: use StreamEventType constants in model_streamer"
```

---

## Task 4: Use constants in `internal/claude/stream.go` (producer)

**Files:**
- Modify: `internal/claude/stream.go` (lines 73, 81, 88, 94, 101, 108, 115, 123, 130, 136)

- [ ] **Step 1: Replace each producer literal with its constant**

These are inside `agent.ApiStreamEvent{...}` composite literals in package `claude`, which already imports `agent` (see `internal/claude/stream.go:10`). Qualify constants as `agent.EventX`:

- Line 73: `EventType:           "message_start",` → `EventType:           agent.EventMessageStart,`
- Line 81: `EventType:    "content_block_start",` → `EventType:    agent.EventContentBlockStart,`
- Line 88: `EventType:  "content_block_start",` → `EventType:  agent.EventContentBlockStart,`
- Line 94: `EventType:          "content_block_start",` → `EventType:          agent.EventContentBlockStart,`
- Line 101: `EventType: "content_block_start",` → `EventType: agent.EventContentBlockStart,`
- Line 108: `EventType:     "content_block_delta",` → `EventType:     agent.EventContentBlockDelta,`
- Line 115: `EventType:     "content_block_delta",` → `EventType:     agent.EventContentBlockDelta,`
- Line 123: `EventType: "content_block_delta",` → `EventType: agent.EventContentBlockDelta,`
- Line 130: `EventType:         "content_block_stop",` → `EventType:         agent.EventContentBlockStop,`
- Line 136: `EventType:    "message_delta",` → `EventType:    agent.EventMessageDelta,`

(The `switch envelope.Type { case "message_start": ... }` cases parse the raw provider payload string and are NOT the `EventType` field — leave them as string literals. The `message_stop` case emits `agent.ApiStreamEvent{Done: true}` with no `EventType` — leave unchanged.)

- [ ] **Step 2: Verify build**

Run: `go build ./internal/claude/`
Expected: no output (success).

- [ ] **Step 3: Run claude tests**

Run: `go test ./internal/claude/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/claude/stream.go
git commit -m "refactor: use StreamEventType constants in claude stream producer"
```

---

## Task 5: Use constants in the aiden producers

**Files:**
- Modify: `internal/aiden/stream.go` (lines 96, 103, 112, 141, 147, 171, 181, 187, 196, 211, 217, 230, 239, 256, 264, 268, 300)
- Modify: `internal/aiden/responses_stream.go` (lines 159, 182, 196, 205, 216, 221, 230, 235, 243, 249, 255, 264, 267, 276, 302, 329, 339)

Both files are in package `aiden` and already import `agent` (`internal/aiden/stream.go:9`, `internal/aiden/responses_stream.go:10`), so qualify as `agent.EventX`.

- [ ] **Step 1: Replace producer literals in `internal/aiden/stream.go`**

Map each `EventType` literal by value (all occurrences on the listed lines):

- `"message_start"` → `agent.EventMessageStart` (lines 171)
- `"message_delta"` → `agent.EventMessageDelta` (lines 112, 268, 300)
- `"content_block_start"` → `agent.EventContentBlockStart` (lines 181, 211, 230)
- `"content_block_delta"` → `agent.EventContentBlockDelta` (lines 187, 217, 239)
- `"content_block_stop"` → `agent.EventContentBlockStop` (lines 96, 103, 141, 147, 196, 256, 264)

Lines 103, 147, 264 are the inline form `out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}` → `out <- agent.ApiStreamEvent{EventType: agent.EventContentBlockStop, Index: idx}`.

- [ ] **Step 2: Replace producer literals in `internal/aiden/responses_stream.go`**

- `"message_start"` → `agent.EventMessageStart` (lines 159, 216, 243, 264 — note 216/243/264 are inline `out <- agent.ApiStreamEvent{EventType: "message_start"}` → `out <- agent.ApiStreamEvent{EventType: agent.EventMessageStart}`)
- `"message_delta"` → `agent.EventMessageDelta` (line 302)
- `"content_block_start"` → `agent.EventContentBlockStart` (lines 182, 196, 205, 230, 249)
- `"content_block_delta"` → `agent.EventContentBlockDelta` (lines 235, 255, 267)
- `"content_block_stop"` → `agent.EventContentBlockStop` (lines 221, 276, 329, 339 — line 339 is the inline `out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}`)

- [ ] **Step 3: Verify build**

Run: `go build ./internal/aiden/`
Expected: no output (success).

- [ ] **Step 4: Run aiden tests**

Run: `go test ./internal/aiden/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/aiden/stream.go internal/aiden/responses_stream.go
git commit -m "refactor: use StreamEventType constants in aiden producers"
```

---

## Task 6: Use constants in remaining consumers (observatory + recording)

**Files:**
- Modify: `internal/observatory/store.go:552` (comparisons — package `observatory`, uses `protocol` constants)
- Modify: `internal/evals/recording/recording.go:44,47` (comparisons — package `recording`, uses `agent` constants)

- [ ] **Step 1: Replace comparisons in `internal/observatory/store.go`**

Line 552 currently:
```go
		if e.EventType == "content_block_delta" || (e.EventType == "message_delta" && e.Detail != "stop_reason") {
```
`e` here is a `protocol.StreamEventDetail`, so use `protocol.EventX` (package `observatory` already imports `protocol` — see `internal/observatory/store.go:13`):
```go
		if e.EventType == protocol.EventContentBlockDelta || (e.EventType == protocol.EventMessageDelta && e.Detail != "stop_reason") {
```

- [ ] **Step 2: Replace comparisons in `internal/evals/recording/recording.go`**

`ev` is an `agent.ApiStreamEvent` (package `recording` already imports `agent` — see `internal/evals/recording/recording.go:6`).

Line 44:
```go
			if ev.EventType == "message_start" && ev.InputTokens > 0 {
```
→
```go
			if ev.EventType == agent.EventMessageStart && ev.InputTokens > 0 {
```

Line 47:
```go
			if ev.EventType == "message_delta" && ev.OutputTokens > 0 {
```
→
```go
			if ev.EventType == agent.EventMessageDelta && ev.OutputTokens > 0 {
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 4: Run affected tests**

Run: `go test ./internal/observatory/... ./internal/evals/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/observatory/store.go internal/evals/recording/recording.go
git commit -m "refactor: use StreamEventType constants in observatory and recording consumers"
```

---

## Task 7: Final full-suite verification

**Files:** none (verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 2: Vet**

Run: `go vet ./...`
Expected: no output (success).

- [ ] **Step 3: Full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 4: Confirm no stray `EventType == "` magic strings remain in non-test production code**

Run: `grep -rn 'EventType\s*[:=]=*\s*"' internal --include='*.go' | grep -v '_test.go' | grep -v 'cassette.go'`
Expected: no output. (Any hit means a producer/consumer literal was missed. `cassette.go` is excluded because its DTO field is intentionally a JSON `string`; `_test.go` files are excluded — see note below.)

---

## Notes on test files (intentionally left as string literals)

Test and fixture files (`internal/agent/turn_bootstrap_test.go`, `internal/aiden/*_test.go`, `internal/claude/stream_test.go` remaining lines, `internal/engine/engine_test.go`, `internal/evals/**/*_test.go`, `internal/testkit/scripted_client.go`, `internal/testkit/e2e_happy_path_test.go`, `internal/agent/compactor_test.go`, `internal/agent/model_streamer_test.go`, `internal/observatory/store_server_test.go`) construct events and compare with string literals like `EventType: "content_block_stop"` and `e.EventType == "content_block_stop"`. These keep compiling unchanged because untyped string constants auto-convert to `StreamEventType` and a named string type compares cleanly against an untyped string literal. They are deliberately left alone to minimize churn and because the literals double as human-readable wire documentation in the fixtures. Only the two `var x string = chunk.EventType` assignments in `claude/stream_test.go` required a change (done in Task 2, Step 6).

---

## Self-Review

**1. Spec coverage.** The request: "how can `EventType` be a `string`?" → give it a real type. Covered: Task 1 defines `agent.StreamEventType` + `protocol.StreamEventType` with constants; Task 2 retypes all three struct fields (`ApiStreamEvent`, `agent.StreamEventDetail`, `protocol.StreamEventDetail`) and repairs every conversion boundary; Tasks 3-6 replace magic-string literals with the constants at every non-test producer and consumer; Task 7 proves no production literals remain. `Detail` is explicitly scoped out with rationale.

**2. Placeholder scan.** Every code step shows exact old→new text and exact file:line. No "TBD", no "handle edge cases", no "similar to Task N". The one repetitive block (producer literal swaps) is expanded per-line with the concrete constant.

**3. Type consistency.** Type name `StreamEventType` and the six constant names (`EventMessageStart`, `EventMessageDelta`, `EventMessageStop`, `EventContentBlockStart`, `EventContentBlockDelta`, `EventContentBlockStop`) are spelled identically everywhere, in both `agent` and `protocol` packages (mirror pattern matching the existing `ApiContentBlockType`/`ContentBlockType` precedent). Cross-package assignments use explicit conversions (`protocol.StreamEventType(...)`, `agent.StreamEventType(...)`, `string(...)`), consistent with the adapter's existing `protocol.ContentBlockType(b.Type)` conversion at `internal/agent/adapter.go:241`. The cassette JSON DTO field stays `string` (with `string(...)`/`agent.StreamEventType(...)` conversions) so on-disk cassette format is unchanged. IPC/JSON wire format for `protocol.StreamEventDetail` is unchanged because a named string type marshals identically to `string`.
