# Built-in Agents Prompt and Profile Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 interactive 主 agent 注入 built-in agents 使用策略，并为 `research/coding/review/execution` 子 agent 注入轻量 profile guidance，同时补齐 prompt 组装测试并验证相关包测试通过。

**Architecture:** 保持 `internal/prompt/system.md` 作为共享 stable base prompt，不复制 4 份完整系统 prompt。通过在 `internal/prompt/system.go` 增加 interactive guidance 与 profile guidance 组装层，把“主 agent 怎么选 built-in agents”和“子 agent 按什么角色做事”集中在 prompt 层表达；runtime 继续负责默认 effort / max_turns 等执行策略。

**Tech Stack:** Go, embed, internal/prompt, internal/runtime, internal/tool, internal/engine, go test

---

## File Structure

- Modify: `internal/prompt/system.go`
  - 增加 interactive guidance 常量与按 profile 选择 guidance 的组装逻辑。
  - 扩展 sub-agent prompt 组装函数签名，使其能接收 profile。
- Modify: `internal/prompt/system.md`
  - 保持共享 stable base prompt；仅在必要时做最小补充，不把 built-in agents 规则直接塞进 base prompt。
- Create: `internal/prompt/system_test.go`
  - 覆盖 interactive prompt 注入、sub-agent profile guidance 注入、extra instructions 追加。
- Modify: `internal/runtime/builder.go`
  - 让 interactive runtime 使用新的 interactive prompt 组装入口。
- Modify: `internal/runtime/runtime.go`
  - 在 sub-agent runtime 构建时，把真实 profile 传给新的 sub-agent prompt 组装入口。
- Modify: `internal/runtime/builder_test.go`
  - 同步断言新的 prompt 组装行为。

## Reuse

- 复用 `FormatStableSystemPrompt(...)` 作为共享 base prompt 入口。
- 复用现有 `AgentProfile` / `ProfileName`，不引入第二套 profile 概念。
- 复用现有 runtime 构建链路，只在 prompt 组装点扩展 profile 信息。
- 复用现有 `go test ./internal/prompt ./internal/runtime ./internal/tool ./internal/engine/...` 作为验证命令。

## Implementation Tasks

### Task 1: 为 interactive prompt 建立 built-in agents guidance 入口

**Files:**
- Modify: `internal/prompt/system.go`
- Test: `internal/prompt/system_test.go`

- [ ] **Step 1: 写 interactive prompt 的失败测试**

```go
func TestFormatInteractiveSystemPromptIncludesBuiltInAgentGuidance(t *testing.T) {
	prompt := FormatInteractiveSystemPrompt("/repo")
	for _, want := range []string{
		"built-in agents",
		"research",
		"coding",
		"review",
		"execution",
		"independent subtasks",
		"agent_type",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("interactive prompt missing %q:\n%s", want, prompt)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/prompt -run TestFormatInteractiveSystemPromptIncludesBuiltInAgentGuidance -v
```

Expected: FAIL，报 `undefined: FormatInteractiveSystemPrompt`。

- [ ] **Step 3: 写最小实现**

在 `internal/prompt/system.go` 增加 interactive guidance 常量与组装函数：

```go
const interactiveBuiltInAgentsGuidance = `When a task is an independent subtask, parallelizable, long-running, or better handled in the background, consider using Agent.

Built-in agents:
- research: search, read, summarize, investigate
- coding: implement, fix, update code, add focused tests
- review: inspect changes, verify behavior, find risks
- execution: run, wait, follow up, drive background progress

When starting an agent, choose the correct agent_type explicitly.`

func FormatInteractiveSystemPrompt(repoRoot string) string {
	base := FormatStableSystemPrompt(repoRoot)
	return base + "\n\n" + strings.TrimSpace(interactiveBuiltInAgentsGuidance)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/prompt -run TestFormatInteractiveSystemPromptIncludesBuiltInAgentGuidance -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/prompt/system.go internal/prompt/system_test.go
git commit -m "feat: add interactive built-in agent guidance"
```

### Task 2: 为 sub-agent prompt 注入 profile guidance

**Files:**
- Modify: `internal/prompt/system.go`
- Test: `internal/prompt/system_test.go`

- [ ] **Step 1: 写 research/coding/review/execution 的失败测试**

```go
func TestFormatSubAgentSystemPromptIncludesProfileGuidance(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{profile: "research", want: "collect evidence before concluding"},
		{profile: "coding", want: "keep code changes focused"},
		{profile: "review", want: "inspect for risks and omissions"},
		{profile: "execution", want: "drive progress and report status"},
	}

	for _, tc := range cases {
		prompt := FormatSubAgentSystemPrompt("/repo", tc.profile, "")
		if !strings.Contains(prompt, tc.want) {
			t.Fatalf("profile %s missing %q:\n%s", tc.profile, tc.want, prompt)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/prompt -run TestFormatSubAgentSystemPromptIncludesProfileGuidance -v
```

Expected: FAIL，因为 `FormatSubAgentSystemPrompt` 签名还不接收 profile。

- [ ] **Step 3: 写最小实现**

在 `internal/prompt/system.go` 增加 profile guidance 常量与选择函数，并修改 `FormatSubAgentSystemPrompt`：

```go
func subAgentProfileGuidance(profile string) string {
	switch strings.TrimSpace(profile) {
	case "research":
		return "Focus on searching, reading, and summarizing. Collect evidence before concluding."
	case "coding":
		return "Focus on implementation work. Keep code changes focused and avoid drifting into open-ended research."
	case "review":
		return "Focus on inspection and verification. Inspect for risks and omissions before approving conclusions."
	case "execution":
		return "Focus on driving progress, waiting when needed, and reporting status clearly."
	default:
		return ""
	}
}

func FormatSubAgentSystemPrompt(repoRoot string, profile string, systemPromptExtra string) string {
	parts := []string{FormatStableSystemPrompt(repoRoot)}
	if guidance := strings.TrimSpace(subAgentProfileGuidance(profile)); guidance != "" {
		parts = append(parts, guidance)
	}
	if extra := strings.TrimSpace(systemPromptExtra); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/prompt -run TestFormatSubAgentSystemPromptIncludesProfileGuidance -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/prompt/system.go internal/prompt/system_test.go
git commit -m "feat: add sub-agent profile guidance"
```

### Task 3: 保证 stable base prompt 与 extra instructions 组装不被破坏

**Files:**
- Test: `internal/prompt/system_test.go`
- Modify: `internal/prompt/system.go`

- [ ] **Step 1: 写 base prompt 与 extra instructions 的失败测试**

```go
func TestFormatSubAgentSystemPromptKeepsBaseAndExtraInstructions(t *testing.T) {
	base := FormatStableSystemPrompt("/repo")
	prompt := FormatSubAgentSystemPrompt("/repo", "coding", "follow repo conventions")

	if !strings.Contains(prompt, strings.Split(base, "\n")[0]) {
		t.Fatalf("sub-agent prompt should keep stable base:\n%s", prompt)
	}
	if !strings.Contains(prompt, "follow repo conventions") {
		t.Fatalf("sub-agent prompt missing extra instructions:\n%s", prompt)
	}
}
```

- [ ] **Step 2: 运行测试确认失败或覆盖到错误行为**

Run:
```bash
go test ./internal/prompt -run TestFormatSubAgentSystemPromptKeepsBaseAndExtraInstructions -v
```

Expected: 如果实现遗漏 base 或 extra，则 FAIL；否则先确认测试通过并保留。

- [ ] **Step 3: 修正实现（如需要）**

如果测试失败，确保 `FormatSubAgentSystemPrompt` 使用统一拼接逻辑：

```go
parts := []string{FormatStableSystemPrompt(repoRoot)}
if guidance := strings.TrimSpace(subAgentProfileGuidance(profile)); guidance != "" {
	parts = append(parts, guidance)
}
if extra := strings.TrimSpace(systemPromptExtra); extra != "" {
	parts = append(parts, extra)
}
return strings.Join(parts, "\n\n")
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/prompt -run TestFormatSubAgentSystemPromptKeepsBaseAndExtraInstructions -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/prompt/system.go internal/prompt/system_test.go
git commit -m "test: cover prompt base and extra instruction composition"
```

### Task 4: 把 interactive runtime 切到新的 interactive prompt 入口

**Files:**
- Modify: `internal/runtime/builder.go`
- Test: `internal/runtime/builder_test.go`

- [ ] **Step 1: 写 interactive runtime prompt 的失败测试**

在 `internal/runtime/builder_test.go` 增加断言：

```go
assembled := interactive.Assembler.Assemble(prompt.TurnContext{})
for _, want := range []string{"built-in agents", "research", "coding", "review", "execution"} {
	if !strings.Contains(assembled.FullText, want) {
		t.Fatalf("interactive prompt missing %q:\n%s", want, assembled.FullText)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/runtime -run TestBuilderBuildsInteractiveAndTaskAgentProfiles -v
```

Expected: FAIL，因为 interactive runtime 还在使用旧的 stable prompt 入口。

- [ ] **Step 3: 写最小实现**

在 `internal/runtime/builder.go` 中，把 interactive profile 的系统 prompt 组装切到新的入口。目标代码形态：

```go
systemPrompt := prompt.FormatStableSystemPrompt(b.shared.ProjectDir)
if req.Profile.Name == ProfileInteractive {
	systemPrompt = prompt.FormatInteractiveSystemPrompt(b.shared.ProjectDir)
}
if req.Profile.Name != ProfileInteractive {
	systemPrompt = prompt.FormatSubAgentSystemPrompt(b.shared.ProjectDir, string(req.Profile.Name), req.SystemPromptExtra)
}
```

如果当前 builder 已有 prompt 组装变量，直接在原位置最小替换，不新增平行路径。

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/runtime -run TestBuilderBuildsInteractiveAndTaskAgentProfiles -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/builder.go internal/runtime/builder_test.go
git commit -m "feat: use interactive prompt guidance in runtime builder"
```

### Task 5: 把 sub-agent runtime 切到按 profile 组装 prompt

**Files:**
- Modify: `internal/runtime/builder.go`
- Modify: `internal/runtime/runtime.go`
- Test: `internal/runtime/builder_test.go`

- [ ] **Step 1: 写 sub-agent runtime prompt 的失败测试**

在 `internal/runtime/builder_test.go` 增加断言：

```go
assembled := coding.Assembler.Assemble(prompt.TurnContext{})
for _, want := range []string{"implementation work", "focused", "worker-only-instructions"} {
	if !strings.Contains(assembled.FullText, want) {
		t.Fatalf("coding prompt missing %q:\n%s", want, assembled.FullText)
	}
}
```

并为 research runtime 增加一条断言：

```go
if !strings.Contains(rt.Assembler.Assemble(prompt.TurnContext{}).FullText, "Collect evidence before concluding") {
	t.Fatalf("research prompt missing profile guidance")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/runtime -run 'TestBuilderBuildsInteractiveAndTaskAgentProfiles|TestSubAgentFactoryFallsBackToDefaultModel' -v
```

Expected: FAIL，因为 sub-agent prompt 还没有按 profile 注入 guidance。

- [ ] **Step 3: 写最小实现**

在 `internal/runtime/builder.go` 的 prompt 组装位置统一改成：

```go
systemPrompt := prompt.FormatStableSystemPrompt(b.shared.ProjectDir)
if req.Profile.Name == ProfileInteractive {
	systemPrompt = prompt.FormatInteractiveSystemPrompt(b.shared.ProjectDir)
} else {
	systemPrompt = prompt.FormatSubAgentSystemPrompt(
		b.shared.ProjectDir,
		string(req.Profile.Name),
		req.SystemPromptExtra,
	)
}
```

并确保 `internal/runtime/runtime.go` 中 sub-agent 构建传入的 `req.Profile` 就是 `research/coding/review/execution` 之一，不再额外拼 prompt。

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/runtime -run 'TestBuilderBuildsInteractiveAndTaskAgentProfiles|TestSubAgentFactoryFallsBackToDefaultModel' -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/builder.go internal/runtime/runtime.go internal/runtime/builder_test.go
git commit -m "feat: inject profile guidance into sub-agent prompts"
```

### Task 6: 跑完整相关验证

**Files:**
- Modify: `internal/prompt/system_test.go`
- Modify: `internal/runtime/builder_test.go`
- Verify: `internal/tool`, `internal/engine`

- [ ] **Step 1: 运行 prompt 包测试**

Run:
```bash
go test ./internal/prompt -v
```

Expected: PASS

- [ ] **Step 2: 运行 runtime 包测试**

Run:
```bash
go test ./internal/runtime -v
```

Expected: PASS

- [ ] **Step 3: 运行 tool 与 engine 相关测试**

Run:
```bash
go test ./internal/tool ./internal/engine/... -v
```

Expected: PASS

- [ ] **Step 4: 运行联合验证命令**

Run:
```bash
go test ./internal/prompt ./internal/runtime ./internal/tool ./internal/engine/...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/prompt/system.go internal/prompt/system_test.go internal/runtime/builder.go internal/runtime/runtime.go internal/runtime/builder_test.go
git commit -m "test: verify built-in agent prompt guidance integration"
```

## Verification Checklist

- [ ] `FormatInteractiveSystemPrompt(...)` 存在，并注入 built-in agents 使用策略。
- [ ] `FormatSubAgentSystemPrompt(..., profile, extra)` 会按 `research/coding/review/execution` 注入不同 guidance。
- [ ] stable base prompt 仍然被复用，没有复制 4 份完整系统 prompt。
- [ ] `systemPromptExtra` 仍然会被追加到 sub-agent prompt。
- [ ] interactive runtime 使用 interactive guidance。
- [ ] sub-agent runtime 使用 profile guidance。
- [ ] `go test ./internal/prompt ./internal/runtime ./internal/tool ./internal/engine/...` 通过。

## Risks

- 如果把 built-in agents guidance 直接塞进 `system.md`，会让所有 prompt 都带上主 agent 决策规则；本计划避免这样做，interactive guidance 单独组装。
- 如果 profile guidance 写得太长，会稀释 stable base prompt；实现时保持 guidance 为短规则片段。
- 修改 `FormatSubAgentSystemPrompt` 签名会影响调用点；本计划通过 runtime/builder 一次性收敛所有调用点并用测试兜住。

## Non-goals

- 不做 4 个 profiles 的重型工具权限切分。
- 不引入用户自定义 agent profiles。
- 不修改 observatory / UI 展示模型。
- 不开放子 agent 再次调用 `Agent`。
