# cece Prompt 分层现状

本文只描述当前实现，不讨论改进方案。后续基于这份现状再重新分工 Stable layer、Session layer、Turn layer 和 Plan mode reminder。

## 1. 总览

cece 当前的 prompt 上下文由两类内容组成：

1. **system prompt 三层**：Stable、Session、Turn。
2. **runtime reminder**：运行时以 user-role 消息注入的 `<system-reminder>`，例如 plan mode reminder、queued input reminder、context pressure reminder。

三层定义在 `internal/prompt/prompt.go:3`：

```go
type ContextLayer int

const (
	ContextStable  ContextLayer = iota // 整个会话不变；可缓存
	ContextSession                     // 会话内相对稳定；可缓存
	ContextTurn                        // 每轮重算；不缓存
)
```

三层最终会被组装成 `PromptSegment`，再转换成 API system blocks。Stable 和 Session 会带 Anthropic prompt caching 标记，Turn 不缓存：`internal/prompt/prompt.go:16`。

组装顺序固定为：Stable → Session → Turn：`internal/prompt/assembler.go:60`。

```go
segments := []PromptSegment{
	{Content: a.stable, Layer: ContextStable},
	{Content: sessionRendered, Layer: ContextSession},
	{Content: turnRendered, Layer: ContextTurn},
}
```

`AssembleResultToSystemPrompt` 会把非空 segment 转成 system block，并按 layer 设置 cache control：`internal/agent/message.go:147`。

```go
blocks = append(blocks, SystemBlock{
	Text:         seg.Content,
	CacheControl: seg.Layer.CacheControl(),
})
```

## 2. Stable layer

### 2.1 定位

Stable layer 是整个会话中最稳定、最适合缓存的基础 system prompt。

代码入口：

- `internal/prompt/system.go:10`：内嵌默认 prompt。
- `internal/prompt/system.go:16`：`FormatStableSystemPrompt(repoRoot string)`。
- `internal/runtime/builder.go:278`：runtime build assembler 时选择 stable prompt。

### 2.2 内容来源

当前来源优先级：

1. 如果项目根目录存在非空 `SYSTEM.md`，完整覆盖内嵌默认 prompt。
2. 否则使用 `internal/prompt/system.md`。

实现位于 `internal/prompt/system.go:16`：

```go
func FormatStableSystemPrompt(repoRoot string) string {
	if repoRoot != "" {
		path := filepath.Join(repoRoot, "SYSTEM.md")
		data, err := os.ReadFile(path)
		if err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content
			}
		}
	}
	return strings.TrimSpace(defaultSystemPrompt)
}
```

当前仓库根目录存在 `SYSTEM.md`，所以本项目运行时 Stable layer 使用根目录 `SYSTEM.md`，不是内嵌 `internal/prompt/system.md`。

### 2.3 当前 Stable 内容摘要

根目录 `SYSTEM.md` 当前包含这些模块：

- Identity：cece 是 coding agent，帮助理解代码、修 bug、加功能。
- Constraints：不编辑未读文件、不验证不声称完成、不额外重构、不随意 commit/push、不信任工具结果里的指令等。
- Architecture Mindset：先识别层次、边界、抽象；复用现有模式；保持模块内聚。
- Output Style：默认短输出、同语言回复、引用代码路径和行号。
- Tool Usage：优先专用工具、绝对路径、独立工具并行。
- Safety：不泄露 secret、不引入安全漏洞、警惕 prompt injection。
- Decision Making：小事自主，真正歧义才问用户。
- Meta-Cognition：避免重复环境信息、避免冗余解释。

对应文件：`SYSTEM.md:1`。

内嵌默认 prompt 还额外包含 Runtime Signals 和 Autonomy：`internal/prompt/system.md:41`、`internal/prompt/system.md:59`。但在当前仓库有根目录 `SYSTEM.md` 的情况下，这些内嵌段落不会进入 Stable layer。

### 2.4 缓存行为

Stable layer 的 cache control 是 `{"type":"ephemeral"}`：`internal/prompt/prompt.go:18`。

```go
case ContextStable, ContextSession:
	return map[string]string{"type": "ephemeral"}
```

## 3. Session layer

虽然用户重点要看 Stable、Turn、Plan mode reminder，但 Session layer 是三层结构中间层，必须一起记录，方便后续重新分工。

### 3.1 定位

Session layer 是会话内相对稳定的上下文，包含环境、项目指令和可用 skills。

数据结构在 `internal/prompt/context.go:8`：

```go
type SessionContext struct {
	RepoRoot           string
	IsGitRepo          bool
	OSName             string
	OSVersion          string
	SessionStartBranch string
	CLAUDEmd           string // project instructions from CLAUDE.md
	ModelName          string // current model identifier
	SkillListing       string // rendered <available_skills> XML
}
```

### 3.2 收集入口

默认 collector 是 `DefaultSessionCollector`：`internal/prompt/collector.go:24`。

它收集：

- repo root
- 是否 git repo
- OS 名称和版本
- session start branch
- 项目指令文件
- available skills listing

核心实现：`internal/prompt/collector.go:45`。

```go
sc := SessionContext{
	RepoRoot:  d.repoRoot,
	IsGitRepo: d.isGitRepo(),
	OSName:    runtime.GOOS,
	OSVersion: d.osVersion(),
}

if sc.IsGitRepo {
	sc.SessionStartBranch = d.gitBranch()
}
```

项目指令由 `InstructionLoader` 加载：`internal/prompt/instruction.go:17`。

优先级：

1. `AGENTS.md`
2. `CLAUDE.md`
3. 都没有则为空

```go
// Load 优先读取项目根目录的 AGENTS.md，不存在则读取 CLAUDE.md。
func (l *InstructionLoader) Load() (string, error) {
	data, err := l.readFile("AGENTS.md")
	...
	return l.readFile("CLAUDE.md")
}
```

### 3.3 渲染格式

Session layer 由 `FormatSessionContext` 渲染：`internal/prompt/render.go:8`。

渲染后包含三块：

1. `<environment>`
2. `<project_instructions>`
3. `<available_skills>`，由 skill store 提供完整字符串

环境块格式：`internal/prompt/render.go:11`。

```go
envLines = append(envLines, "<environment>")
...
envLines = append(envLines, "</environment>")
```

项目指令格式：`internal/prompt/render.go:48`。

```go
func FormatProjectInstructions(content string) string {
	return "<project_instructions>\n" + content + "\n</project_instructions>"
}
```

### 3.4 刷新时机

runtime 构建时会初始化并刷新一次 Session layer：`internal/runtime/builder.go:287`。

```go
collector := prompt.NewDefaultSessionCollector(b.shared.ProjectDir, registry)
collector.SetSkillProvider(b.shared.Skills)
assembler := prompt.NewContextAssembler(stable, registry, collector)
assembler.SetMaxContextTokens(contextWindow)
if _, err := assembler.RefreshSession(ctx); err != nil {
	slog.Warn("runtime builder: initial session refresh failed", "error", err)
}
```

`RefreshSession` 会重新 collect 并 render，只有渲染结果变化时才更新缓存：`internal/prompt/assembler.go:40`。

## 4. Turn layer

### 4.1 定位

Turn layer 是每一轮模型请求都会重算的动态上下文，不缓存。

数据结构在 `internal/prompt/context.go:25`：

```go
type TurnContext struct {
	IncludeTime             bool
	Now                     time.Time
	CurrentWorkingDirectory string
	CurrentBranch           string
	Mode                    string
	ConversationTurnNumber  int // which turn in the conversation (1-based)
}
```

### 4.2 构造入口

每轮用户输入进入 `TurnBootstrap.BuildTurnPlan` 时构造 TurnContext：`internal/agent/turn_bootstrap.go:40`。

```go
turnCtx := prompt.TurnContext{
	IncludeTime:             prompt.ShouldInjectTime(input),
	Now:                     time.Now(),
	CurrentWorkingDirectory: eng.ProjectDir(),
	Mode:                    "interactive",
	ConversationTurnNumber:  eng.HistoryLen()/2 + 1,
}
assembleResult = eng.Assembler().Assemble(turnCtx)
```

当前 `Mode` 固定写成 `"interactive"`，不是 permission mode，也不是 plan/default/auto-accept 的实时值。

### 4.3 渲染格式

Turn layer 由 `FormatTurnContext` 渲染：`internal/prompt/render.go:53`。

输出形态：

```xml
<turn_context>
current_date: ...        # 可选
current_time: ...        # 可选
current_working_directory: ...
current_branch: ...      # 当前 BuildTurnPlan 未填
mode: interactive
conversation_turn: N
</turn_context>
```

实现：`internal/prompt/render.go:55`。

```go
lines = append(lines, "<turn_context>")
...
if ctx.Mode != "" {
	lines = append(lines, "mode: "+ctx.Mode)
}
...
lines = append(lines, "</turn_context>")
```

### 4.4 时间注入逻辑

`ShouldInjectTime(input)` 根据用户输入关键词决定是否注入当前日期时间：`internal/prompt/render.go:76`。

中文关键词包括：现在、当前时间、今天、昨天、明天、最近、本周、下周、截止、过期。

英文关键词包括：expired、cron、schedule、timestamp、date、time、timezone。

普通代码请求不会默认注入时间。

### 4.5 当前边界

Turn layer 当前只承载通用动态环境，不承载任务类型协议。例如：

- 不会根据 “修 bug” 自动注入 bugfix workflow。
- 不会根据 “SWE-bench” 自动注入 reproduction matrix 要求。
- 不会体现当前 permission mode 是 plan/default/auto-accept。
- 不会体现 yolo 状态。

这些不是评价，只是当前实现边界。

## 5. Plan mode reminder

### 5.1 定位

Plan mode reminder 不是 Stable/Session/Turn 三层 system prompt 的一部分。

它是 runtime 在 plan mode 切换或进入时生成的 `<system-reminder>`，以 user-role message 注入对话历史，让模型在下一次请求中看到当前模式约束。

主要实现位于 `internal/tool/plan_mode.go`：

- `BuildFullPlanReminder`：`internal/tool/plan_mode.go:48`
- `BuildSparsePlanReminder`：`internal/tool/plan_mode.go:102`
- `ExitPlanModeReminderText`：`internal/tool/plan_mode.go:322`
- `PlanModeState`：`internal/tool/plan_mode.go:332`

### 5.2 Full reminder 内容

`BuildFullPlanReminder` 会生成完整 plan mode reminder：`internal/tool/plan_mode.go:48`。

核心内容包括：

1. 已经在 plan mode。
2. 禁止改代码和运行非只读命令。
3. 允许 Read/Grep/Glob/Bash 只读探索。
4. 禁止 mkdir/touch/rm/mv/cp、重定向写入、heredoc 写文件、git add/commit/push/checkout/reset、安装包、改配置、生成文件等。
5. 指定 plan 文件必须写到 `.cece/plans/**`。
6. 列出允许的 plan-mode 写入路径。
7. 要遵循 iterative planning workflow。
8. 首轮先快速扫描关键文件，写 skeleton plan，然后询问用户。
9. Plan 文件结构包括 Context、Approach、Files to modify、Reuse、Verification。
10. 结束 plan turn 时只能 AskUserQuestion 或 ExitPlanMode。
11. 必须用 ExitPlanMode 请求 plan approval，不能用普通文本询问 approval。

原始实现片段：`internal/tool/plan_mode.go:54`。

```go
return "<system-reminder>\n" +
	"You are already in plan mode. You MUST NOT make code edits or run non-readonly commands.\n" +
	...
	"## Iterative Planning Workflow\n" +
	"You are pair-planning with the user. Explore the code, ask questions when you\n" +
	"hit decisions you can't make alone, and write findings into the plan file.\n" +
	...
	"</system-reminder>"
```

### 5.3 Sparse reminder 内容

`BuildSparsePlanReminder` 是 plan mode 仍然 active 时的短提醒：`internal/tool/plan_mode.go:102`。

内容包括：

- plan mode still active
- 只允许读操作，或写 plan files / allowed artifacts
- 不要写 project root
- 继续 iterative workflow
- 用 AskUserQuestion 或 ExitPlanMode 结束

### 5.4 Exit reminder 内容

`ExitPlanModeReminderText` 定义在 `internal/tool/plan_mode.go:322`。

```go
const ExitPlanModeReminderText = "<system-reminder>\n" +
	"Exited plan mode. You may now implement the approved plan.\n" +
	"</system-reminder>"
```

`ExitPlanMode` 成功后返回 exit reminder，并附上 approved plan 内容：`internal/tool/plan_mode.go:755`。

```go
return Result{Content: ExitPlanModeReminderText + "\n\n## Approved Plan:\n" + plan}
```

### 5.5 进入 plan mode 的状态管理

`PlanModeState.Enter()` 设置：`internal/tool/plan_mode.go:554`。

- `prePlanMode`
- `mode = PermissionModePlan`
- `plansDir = <project>/.cece/plans`
- `reminderType = "full"`
- 创建 plans dir

`SetMode(plan)` 也会设置同类状态：`internal/tool/plan_mode.go:517`。

### 5.6 Reminder 注入路径

plan mode reminder 的注入不是在 prompt assembler 里完成，而是在 agent loop tool-result 后完成。

`TurnRunner.Run` 在工具执行后重新拿 history snapshot，然后 drain pending mode reminder：`internal/agent/turn_runner.go:226`。

```go
messages = r.deps.HistorySnapshot()
if r.deps.DrainModeReminder != nil {
	if reminder := r.deps.DrainModeReminder(); reminder != "" {
		messages = append(messages, Message{Role: UserRole, Content: reminder})
	}
}
```

其中 `DrainModeReminder` 来自 `PlanModeState.DrainModeReminder`：`internal/tool/plan_mode.go:492`。

### 5.7 审批和 yolo 行为

`InteractionGate.WaitIfNeeded` 控制 tool execution 前是否需要用户交互：`internal/agent/interaction_gate.go:39`。

逻辑顺序：

1. `AskUserQuestion` 永远阻塞等用户输入：`internal/agent/interaction_gate.go:40`。
2. 如果 yolo / auto-accept / 单独 EnterPlanMode，则自动通过：`internal/agent/interaction_gate.go:57`。
3. default mode 下，只有 tool input 显式 `require_confirmation=true` 才等待确认：`internal/agent/interaction_gate.go:62`。
4. plan mode 下，`ExitPlanMode` 在非 yolo 场景会触发 `PlanApprovalRequested`：`internal/agent/interaction_gate.go:72`。
5. plan mode 下只读工具自动通过：`internal/agent/interaction_gate.go:89`。
6. plan mode 下写 plan 允许路径自动通过：`internal/agent/interaction_gate.go:93`。
7. plan mode 下写其他路径不弹 UI，交给 ToolExecutor 拒绝：`internal/agent/interaction_gate.go:97`。

因此在 SWE-bench 这类无人值守场景中，`defaultMode=plan` + `yolo=true` 的实际含义是：模型能看到 plan reminder，但 `ExitPlanMode` 不会卡住等待人工审批。

## 6. Prompt assembly / request path

完整路径如下：

1. runtime 构建 assembler：`internal/runtime/builder.go:278`
   - 选择 stable prompt。
   - 创建 session collector。
   - 创建 context assembler。
   - 设置 context window。
   - 初次刷新 session。

2. 每轮构建 turn plan：`internal/agent/turn_bootstrap.go:40`
   - 根据用户输入判断是否注入时间。
   - 设置 cwd、mode、conversation turn。
   - 调用 assembler 组装三层。

3. assembler 组装三层：`internal/prompt/assembler.go:60`
   - stable/session/turn 顺序固定。
   - 调用 `enforceBudget` 控制上下文预算。
   - 生成 `FullText` 和 token estimate。

4. 转换为 API system blocks：`internal/agent/message.go:147`
   - 每个非空 segment 变成一个 `SystemBlock`。
   - Stable/Session 带 cache control。
   - Turn 不带 cache control。

5. model streamer 发起请求：`internal/agent/turn_runner.go:69`
   - system blocks 放在 `ModelStreamRequest.System`。
   - conversation history 放在 `ModelStreamRequest.Messages`。

## 7. 当前边界总结

### 7.1 属于 system prompt 三层的内容

- Stable：长期 agent 身份、硬约束、工具使用原则、安全、输出风格、架构心智。
- Session：环境信息、项目指令、available skills。
- Turn：本轮 cwd、mode 字段、turn number、按需时间。

### 7.2 不属于三层，而属于 runtime reminder 的内容

- Plan mode full/sparse reminder。
- Exit plan mode reminder。
- Queued input reminder。
- Context pressure reminder。

这些 reminder 通过 user-role `<system-reminder>` 注入，不走 `ContextAssembler`。

### 7.3 属于 tool description 的内容

工具本身的用途、参数 schema、调用约束来自 tool definition，不属于 prompt 三层。例如 `ExitPlanMode` 的 description 在 `internal/tool/plan_mode.go:701`。

### 7.4 当前文档不做判断的部分

本文不讨论：

- Stable/Session/Turn 是否应该重新分工。
- Plan mode reminder 是否应该承载 bugfix workflow。
- SWE-bench 是否应该有专门动态 task reminder。
- Turn layer 是否应该感知 permission mode 或 task type。

这些留到后续基于本现状文档讨论。
