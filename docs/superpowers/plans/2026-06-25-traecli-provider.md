# TraeCLI Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow cece to use coco/traecli LLM API endpoints through a first-class `traecli` provider protocol.

**Architecture:** Add `traecli` as a protocol alias that reuses the existing OpenAI-compatible Aiden client, including streaming, `/v1/models`, Bearer token handling, optional `authHelper`, and Responses API routing for GPT/O-series models. Keep the change intentionally small: no local coco plugin scanning and no legacy provider dependency.

**Tech Stack:** Go 1.19, standard library tests, existing `internal/aiden` OpenAI-compatible client, setup wizard.

---

### Task 1: Add runtime client factory support

**Files:**
- Modify: `cmd/cece/main.go`
- Create: `cmd/cece/main_test.go`

- [ ] **Step 1: Write the failing test**

Add a test that creates a provider with `Protocol: "traecli"` and verifies the factory returns an `*aiden.Client`.

- [ ] **Step 2: Run test to verify it fails**

Run: `~/sdk/go1.19.13/bin/go test ./cmd/cece -run TestCreateClientTraeCLIUsesOpenAICompatibleClient -count=1`

Expected: FAIL because `traecli` currently falls through to the Anthropic client.

- [ ] **Step 3: Implement factory support**

Add `case "traecli"` next to `aiden`/`bytedance` in `createClient`, using `aiden.NewClient`, `SetAuthHelper`, and `SetUseResponsesAPI` exactly like existing OpenAI-compatible protocols.

- [ ] **Step 4: Run test to verify it passes**

Run: `~/sdk/go1.19.13/bin/go test ./cmd/cece -run TestCreateClientTraeCLIUsesOpenAICompatibleClient -count=1`

Expected: PASS.

### Task 2: Add setup wizard support

**Files:**
- Modify: `internal/setup/setup.go`
- Create: `internal/setup/setup_test.go`

- [ ] **Step 1: Write the failing test**

Add a test that verifies `protocols` contains `traecli`.

- [ ] **Step 2: Run test to verify it fails**

Run: `~/sdk/go1.19.13/bin/go test ./internal/setup -run TestProtocolsIncludeTraeCLI -count=1`

Expected: FAIL because `traecli` is not listed.

- [ ] **Step 3: Implement setup support**

Append `{id: "traecli"}` to the `protocols` slice. Do not special-case baseURL; users enter the endpoint explicitly and `/v1/models` is reused by `fetchModels`.

- [ ] **Step 4: Run test to verify it passes**

Run: `~/sdk/go1.19.13/bin/go test ./internal/setup -run TestProtocolsIncludeTraeCLI -count=1`

Expected: PASS.

### Task 3: Update user-facing protocol documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/settings.example.json`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Update protocol lists**

Document `traecli` as an OpenAI-compatible coco/traecli endpoint protocol. Keep the example concise and do not add a new documentation file.

- [ ] **Step 2: Run focused tests**

Run: `~/sdk/go1.19.13/bin/go test ./cmd/cece ./internal/setup ./internal/config ./internal/aiden -count=1`

Expected: PASS.

### Task 4: Final verification

**Files:**
- No additional files.

- [ ] **Step 1: Format Go files**

Run: `~/sdk/go1.19.13/bin/gofmt -w cmd/cece/main.go cmd/cece/main_test.go internal/setup/setup.go internal/setup/setup_test.go internal/config/config.go`

- [ ] **Step 2: Run focused test suite**

Run: `~/sdk/go1.19.13/bin/go test ./cmd/cece ./internal/setup ./internal/config ./internal/aiden -count=1`

Expected: PASS.
