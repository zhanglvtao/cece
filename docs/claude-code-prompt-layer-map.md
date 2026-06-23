# Claude Code Prompt 到 cece Layer 的映射

本文只整理 Claude Code 当前 prompt 内容分别属于哪一类 layer，作为后续讨论 cece prompt 分工的输入。本文不提出具体改造方案。

参考基线：cece 当前分层见 `docs/prompt-layers-current-state.md:1`。

## 1. 总览

Claude Code 的 prompt 结构不完全等价于 cece 的 Stable / Session / Turn 三层。更准确地说，它有四类模型可见上下文：

1. **Static system prompt**：长期稳定、可缓存的基础行为协议。
2. **Dynamic system sections**：会话、环境、工具、feature gate 相关的动态 system sections。
3. **Runtime attachments / reminders**：以 user meta message 注入的 `<system-reminder>`，例如 plan mode、auto mode、verify plan reminder。
4. **Workflow-specific agent prompts / tool contracts**：例如 verification agent 的独立 system prompt、VerifyPlanExecution tool 的 schema 和 prompt。

如果映射到 cece 当前概念，大致是：

| Claude Code 结构 | Claude Code 入口 | cece 对应 layer | 说明 |
| --- | --- | --- | --- |
| Static system prompt | `src/constants/prompts.ts:513` | Stable layer | 长期基础行为协议，适合进入 cece `SYSTEM.md` 或内嵌 stable prompt。 |
| Dynamic system sections | `src/constants/prompts.ts:455` | Session layer + 少量 Turn layer | 多数是会话稳定信息；少数随 runtime 状态变化。 |
| Plan/Auto reminders | `src/utils/messages.ts:3572` | Plan mode reminder / runtime reminder | 不属于 system 三层，是运行时注入的 mode protocol。 |
| Verification agent | `packages/builtin-tools/src/tools/AgentTool/built-in/verificationAgent.ts:10` | 独立 subagent Stable prompt | 是专门 agent 的 system prompt，不应直接塞入主 agent stable。 |
| VerifyPlanExecution | `packages/builtin-tools/src/tools/VerifyPlanExecutionTool/VerifyPlanExecutionTool.ts:28` | Workflow closure / tool contract | 是计划执行后的闭环工具，不是普通提示文本。 |

一句话：Claude Code 不是只有“分层 prompt”，而是把长期行为、动态上下文、运行时 mode、后置验证工具一起组成了工作流闭环。

## 2. Claude Code 主 system prompt 结构

Claude Code 的主入口是 `getSystemPrompt()`：`/Users/bytedance/claude-code/src/constants/prompts.ts:408`。

它最终返回两类内容：

```ts
return [
  // --- Static content (cacheable) ---
  getSimpleIntroSection(outputStyleConfig),
  getSimpleSystemSection(),
  outputStyleConfig === null ||
  outputStyleConfig.keepCodingInstructions === true
    ? getSimpleDoingTasksSection()
    : null,
  getActionsSection(),
  getUsingYourToolsSection(enabledTools),
  getOutputEfficiencySection(),
  // === BOUNDARY MARKER - DO NOT MOVE OR REMOVE ===
  ...(shouldUseGlobalCacheScope() ? [SYSTEM_PROMPT_DYNAMIC_BOUNDARY] : []),
  // --- Dynamic content (registry-managed) ---
  ...resolvedDynamicSections,
].filter(s => s !== null)
```

这个结构和 cece 最大的差异：

- cece 有 Stable / Session / Turn 三层类型定义。
- Claude Code 以 static / dynamic boundary 为核心，并通过 section registry 控制缓存和重算。
- Claude Code 的 static 部分不仅是原则，还直接包含“怎么做任务”的操作协议。

## 3. Static system prompt：对应 cece Stable layer

Claude Code static prompt 是最接近 cece Stable layer 的部分。

入口：`/Users/bytedance/claude-code/src/constants/prompts.ts:513`。

### 3.1 Static sections 列表

| Static section | 入口 | 内容职责 | cece 映射 |
| --- | --- | --- | --- |
| Intro | `getSimpleIntroSection` | 身份、用途、安全 URL 约束 | Stable / Identity + Safety |
| System | `getSimpleSystemSection` | 工具权限、tool result/system reminder 语义、prompt injection、hooks、自动压缩 | Stable / Runtime Signals + Safety |
| Doing tasks | `getSimpleDoingTasksSection` | 软件工程任务行为、范围控制、验证、失败诊断、真实汇报 | Stable / Coding workflow contract |
| Executing actions with care | `getActionsSection` | 风险动作、可逆性、blast radius、用户确认 | Stable / Safety + Permission behavior |
| Using your tools | `getUsingYourToolsSection` | 工具优先级、搜索、task/todo 管理 | Stable / Tool Usage |
| Communication style | `getOutputEfficiencySection` | 用户沟通风格、状态更新、最终报告 | Stable / Output Style |

### 3.2 Doing tasks：最关键的 Stable 差异

Claude Code 的 `Doing tasks` 是 static system prompt 的核心部分，入口在 `/Users/bytedance/claude-code/src/constants/prompts.ts:204`。

它包含这些长期行为协议：

#### completion counterweight

位置：`/Users/bytedance/claude-code/src/constants/prompts.ts:208`、`/Users/bytedance/claude-code/src/constants/prompts.ts:712`。

核心语义：

```text
The right amount of complexity is what the task actually requires—no speculative abstractions, but no half-finished implementations either.
```

以及 subagent 默认 prompt：

```text
Complete the task fully—don't gold-plate, but don't leave it half-done.
```

归属：**Stable layer**。

原因：这是全局长期行为约束，不依赖当前项目、不依赖某一轮输入，也不只属于 plan mode。

#### verification contract

位置：`/Users/bytedance/claude-code/src/constants/prompts.ts:214`。

核心语义：

```text
Before reporting a task complete, verify it actually works: run the test, execute the script, check the output.
Minimum complexity means no gold-plating, not skipping the finish line.
If you can't verify, say so explicitly rather than claiming success.
```

归属：**Stable layer**。

原因：这是每个实现任务完成前的全局完成契约，不能只放在 plan reminder；否则 auto mode、default mode、subagent、非计划任务都会缺失。

#### failure diagnosis

位置：`/Users/bytedance/claude-code/src/constants/prompts.ts:231`。

核心语义：

```text
If an approach fails, diagnose why before switching tactics—read the error, check your assumptions, try a focused fix.
Don't retry the identical action blindly, but don't abandon a viable approach after a single failure either.
```

归属：**Stable layer**。

原因：这是通用问题处理策略，适用于测试失败、工具失败、构建失败、环境失败，不应绑定到某个 mode。

#### faithful reporting

位置：`/Users/bytedance/claude-code/src/constants/prompts.ts:236`。

核心语义：

```text
Report outcomes faithfully: if tests fail, say so with the relevant output; if you did not run a verification step, say that rather than implying it succeeded.
Never claim "all tests pass" when output shows failures.
```

归属：**Stable layer**。

原因：这是最终汇报契约，和验证契约组成闭环。

### 3.3 Actions：风险动作协议

`getActionsSection()` 入口在 `/Users/bytedance/claude-code/src/constants/prompts.ts:248`。

它不是单纯“安全提醒”，而是行动策略：

- 根据 reversibility 和 blast radius 判断是否需要确认。
- 本地可逆动作可以做。
- 删除、force push、reset hard、影响共享系统、发消息、上传第三方等需要谨慎。
- 遇到障碍不要用破坏性动作绕过，要先定位根因。

归属：**Stable layer**。

对应 cece：Stable 里已有 Safety 和 Tool Usage，但 Claude Code 的这段更像“权限/风险决策协议”。

### 3.4 Using tools：工具行为协议

`getUsingYourToolsSection()` 入口在 `/Users/bytedance/claude-code/src/constants/prompts.ts:262`。

它包含：

- core tools 可以直接调用。
- 优先专用工具，不用 Bash 替代 Read/Edit/Glob/Grep。
- Bash 保留给 shell operations、测试、构建、git。
- 用户提到未知文件/函数/模块时先 search。
- 有 task/todo 工具时要拆解和更新任务。

归属：**Stable layer**，但其中一部分依赖 enabled tools，所以在 Claude Code 中 static section 仍会读取 `enabledTools`。

对应 cece：Stable 已有类似 Tool Usage，但 Claude Code 多了“Search before saying unknown”和 task tool 行为。

### 3.5 Communication style：输出协议

`getOutputEfficiencySection()` 入口在 `/Users/bytedance/claude-code/src/constants/prompts.ts:382`。

内容包括：

- 面向人写，不面向 console。
- 工具调用前简短说明，关键节点更新。
- 不描述内部工具机械过程。
- 完成后报告结果，不追加客套问题。
- 引用代码使用 `file_path:line_number`。

归属：**Stable layer**。

对应 cece：cece Stable 里也有 Output Style，但更短、更强约束“默认 4 行内”；Claude Code 更强调用户体验和状态同步。

## 4. Dynamic system sections：对应 cece Session layer + 部分 Turn layer

Claude Code dynamic sections 定义在 `getSystemPrompt()` 的 `dynamicSections`：`/Users/bytedance/claude-code/src/constants/prompts.ts:455`。

它用 `systemPromptSection()` / `DANGEROUS_uncachedSystemPromptSection()` 管理缓存：`/Users/bytedance/claude-code/src/constants/systemPromptSections.ts:16`。

```ts
export function systemPromptSection(name: string, compute: ComputeFn): SystemPromptSection {
  return { name, compute, cacheBreak: false }
}

export function DANGEROUS_uncachedSystemPromptSection(
  name: string,
  compute: ComputeFn,
  _reason: string,
): SystemPromptSection {
  return { name, compute, cacheBreak: true }
}
```

### 4.1 Dynamic sections 列表和 layer 归属

| Dynamic section | 入口 | 内容职责 | cece 映射 |
| --- | --- | --- | --- |
| `session_guidance` | `getSessionSpecificGuidanceSection` | 根据当前工具、skills、subagent feature、verification agent 注入行为协议 | Session layer；其中工具状态敏感部分也可视作 Turn-adjacent |
| `memory` | `loadMemoryPrompt()` | 用户/项目记忆 | Session layer |
| `ant_model_override` | `getAntModelOverrideSection()` | 内部模型覆盖提示 | Session layer / provider-specific stable |
| `env_info_simple` | `computeSimpleEnvInfo()` | cwd、git、platform、shell、OS、model、cutoff | Session layer；如果每轮 cwd 变化则 Turn layer |
| `language` | `getLanguageSection()` | 用户语言偏好 | Session layer |
| `output_style` | `getOutputStyleSection()` | 输出风格配置 | Session layer |
| `mcp_instructions` | `getMcpInstructionsSection()` | MCP server instructions | Session layer，但 connect/disconnect 会变，Claude Code 标记为 uncached |
| `scratchpad` | `getScratchpadInstructions()` | scratchpad 目录规则 | Session layer |
| `frc` | `getFunctionResultClearingSection()` | tool result clearing 策略 | Session layer / runtime context policy |
| `summarize_tool_results` | constant | 旧 tool result 会被清理时提醒模型记录重要信息 | Session layer / context hygiene |
| `token_budget` | feature gated | token 预算目标 | Turn layer / runtime attachment 候选 |
| `brief` | `getBriefSection()` | brief/proactive 相关行为 | Session layer / runtime mode |

### 4.2 session-specific guidance：动态行为协议

`getSessionSpecificGuidanceSection()` 位于 `/Users/bytedance/claude-code/src/constants/prompts.ts:327`。

它根据当前启用工具和 feature 生成提示，例如：

- 有 AskUserQuestion 时，用户拒绝 tool call 后可询问原因。
- 需要用户自己运行交互式 shell 命令时，提示使用 `! <command>`。
- 有 Agent tool 时，注入 subagent 使用策略。
- 有 skills 时，说明 `/skill-name` 和 Skill tool 使用规则。
- verification agent feature 打开时，注入独立验证契约。

归属：**Session layer 为主**。

原因：它依赖当前 session 的工具集、feature gate、skills，不是长期 stable，也不是单纯每轮输入派生。

其中 verification agent 的触发契约是一个特殊点：

```text
when non-trivial implementation happens on your turn, independent adversarial verification must happen before you report completion
```

这句虽然出现在 session-specific guidance，但语义上是 **workflow closure contract**。它依赖 agent tool 和 feature gate 是否存在，所以 Claude Code 放在 dynamic section，而不是 static stable。

## 5. Runtime attachments / reminders：对应 cece runtime reminder

Claude Code 的 runtime reminders 通过 `wrapMessagesInSystemReminder()` 作为 user meta message 注入，不属于主 system prompt static/dynamic sections。

这和 cece 的 Plan mode reminder 类似：cece 文档已经记录 Plan reminder 不属于三层，而是 runtime `<system-reminder>`：`docs/prompt-layers-current-state.md:306`。

### 5.1 Plan mode V2：5 阶段 workflow

入口：`/Users/bytedance/claude-code/src/utils/messages.ts:3572`。

归属：**Plan mode reminder / runtime reminder**。

不是 Stable，因为它只在 plan mode active 时出现。

核心结构：

1. Phase 1: Initial Understanding
   - 理解用户请求和相关代码。
   - 搜索可复用函数、工具、模式。
   - 可并行启动 explore agents。
2. Phase 2: Design
   - 启动 plan agents 设计实现。
   - 对复杂任务用不同视角考虑，例如 bug fix 的 root cause vs workaround vs prevention。
3. Phase 3: Review
   - 读取关键文件深化理解。
   - 确认计划和用户原始请求对齐。
   - 必要时 AskUserQuestion。
4. Phase 4: Control / Cut / Cap
   - 由 `getPlanPhase4Section()` 插入，控制计划收敛方式。
5. Phase 5: Call ExitPlanMode
   - 最后必须调用 ExitPlanMode。
   - 不允许用普通文本询问 approval。

这比 cece 当前 plan reminder 更强的地方是：它把“理解、设计、review、退出”拆成阶段，并明确要求 review plan 与用户原始意图对齐。

### 5.2 Plan mode interview workflow

入口：`/Users/bytedance/claude-code/src/utils/messages.ts:3688`。

归属：**Plan mode reminder / runtime reminder**。

内容更接近 cece 当前 full plan reminder：

- Explore → Update plan file → Ask user。
- 首轮先快速扫关键文件，写 skeleton plan，再问问题。
- Asking Good Questions：不要问能从代码读出来的问题；批量提问；聚焦用户才能回答的问题。
- Plan File Structure：Context、推荐方案、关键文件、复用函数、验证方案。
- When to Converge：消除歧义，覆盖修改内容、文件、复用、验证后再 ExitPlanMode。

cece 当前 `BuildFullPlanReminder` 已经覆盖了其中一部分，但缺少 “Asking Good Questions” 和 “When to Converge” 的密度。

### 5.3 Sparse reminder

入口：`/Users/bytedance/claude-code/src/utils/messages.ts:3750`。

归属：**Plan mode reminder / runtime reminder**。

它只保留短提示：plan mode still active、只读、plan file、follow workflow、只能 AskUserQuestion 或 ExitPlanMode 结束。

cece 也有类似 `BuildSparsePlanReminder`：`internal/tool/plan_mode.go:102`。

### 5.4 Auto mode reminder

入口：`/Users/bytedance/claude-code/src/utils/messages.ts:3784`。

归属：**runtime reminder**，不是 Plan mode。

内容包括：

- Execute immediately。
- Minimize interruptions。
- Prefer action over planning。
- Expect course corrections。
- Destructive actions 仍需确认。
- Avoid data exfiltration。

对应 cece：如果 cece 未来明确区分 default / auto-accept / yolo 的模型可见语义，这类内容不应放 Stable，而应作为 mode-specific runtime reminder。

## 6. Verification agent / VerifyPlanExecution：workflow closure layer

这部分不是普通三层 prompt，也不是 plan mode reminder。它们是 Claude Code 用来闭环实现质量的 workflow 机制。

### 6.1 Verification agent

入口：`/Users/bytedance/claude-code/packages/builtin-tools/src/tools/AgentTool/built-in/verificationAgent.ts:10`。

归属：**独立 subagent Stable prompt + tool gating + critical runtime reminder**。

它不是主 agent 的 Stable layer，因为它只属于 verification subagent。

它的 system prompt 很强，核心包括：

- 你的任务不是确认实现有效，而是试图破坏它。
- 警惕 verification avoidance：不要只读代码然后写 PASS。
- 警惕 first 80% illusion：不要被表面通过迷惑。
- 严禁修改 project directory。
- 根据变更类型采用不同验证策略。
- Bug fixes：Reproduce original bug → verify fix → run regression tests → check related side effects。
- Test suite results are context, not evidence。
- 必须运行实际命令，报告 command/output/result。
- 结束必须输出 `VERDICT: PASS|FAIL|PARTIAL`。

关键归属判断：

| 内容 | 归属 |
| --- | --- |
| Verification agent 的身份和目标 | Subagent Stable prompt |
| 禁止修改项目 | Subagent Stable prompt + tool gating |
| 各类型验证策略 | Subagent Stable prompt |
| 必须输出 verdict | Subagent output contract |
| 主 agent 何时调用 verifier | Session-specific guidance / workflow closure |

### 6.2 VerifyPlanExecution tool

入口：`/Users/bytedance/claude-code/packages/builtin-tools/src/tools/VerifyPlanExecutionTool/VerifyPlanExecutionTool.ts:28`。

归属：**workflow closure / tool contract**。

它不是提示模型“最好验证”，而是提供一个工具让模型显式声明：

- plan summary
- verification notes
- all steps completed

工具 prompt：`/Users/bytedance/claude-code/packages/builtin-tools/src/tools/VerifyPlanExecutionTool/VerifyPlanExecutionTool.ts:41`。

```text
Verify that a plan has been executed correctly. Call this tool before exiting plan mode to confirm all steps were completed.
```

同时 runtime attachment `verify_plan_reminder` 会要求调用这个工具：`/Users/bytedance/claude-code/src/utils/messages.ts:4654`。

```text
You have completed implementing the plan. Please call the "VerifyPlanExecution" tool directly ... to verify that all plan items were completed correctly.
```

这说明 Claude Code 的验证不是单层 prompt 文本，而是：

1. Stable 里有 verification contract。
2. Session guidance 里可要求 non-trivial implementation 启动 verification agent。
3. Runtime reminder 在计划执行后要求 VerifyPlanExecution。
4. Tool schema 让模型结构化声明是否完成。

## 7. 用户列出的差异项归属表

| 用户列出的点 | Claude Code 文件 | 属于 Claude Code 哪类 | 映射到 cece 哪个 layer |
| --- | --- | --- | --- |
| 主 system prompt 入口 | `src/constants/prompts.ts:408` | Prompt assembler / system prompt entry | cece `ContextAssembler` + runtime builder 对应入口 |
| system prompt sections 缓存 | `src/constants/systemPromptSections.ts:16` | Dynamic section cache manager | cece Session layer cache/refresh 机制的参考 |
| Plan mode reminder | `src/utils/messages.ts:3572` | Runtime `<system-reminder>` attachment | cece Plan mode reminder |
| Doing tasks | `src/constants/prompts.ts:204` | Static system prompt | Stable layer |
| Executing actions with care | `src/constants/prompts.ts:248` | Static system prompt | Stable layer |
| Using tools | `src/constants/prompts.ts:262` | Static system prompt, 但依赖 enabledTools | Stable layer；工具可用性相关可进 Session |
| Communication style | `src/constants/prompts.ts:382` | Static system prompt | Stable layer |
| completion counterweight | `src/constants/prompts.ts:208`, `src/constants/prompts.ts:712` | Static system prompt / subagent prompt | Stable layer；subagent 另有 subagent Stable |
| verification contract | `src/constants/prompts.ts:214` | Static system prompt | Stable layer |
| failure diagnosis | `src/constants/prompts.ts:231` | Static system prompt | Stable layer |
| faithful reporting | `src/constants/prompts.ts:236` | Static system prompt | Stable layer |
| session-specific guidance | `src/constants/prompts.ts:327` | Dynamic system section | Session layer；部分 workflow closure |
| plan mode 5-phase workflow | `src/utils/messages.ts:3572` | Runtime plan reminder | Plan mode reminder |
| plan mode interview workflow | `src/utils/messages.ts:3688` | Runtime plan reminder | Plan mode reminder |
| auto mode reminder | `src/utils/messages.ts:3784` | Runtime mode reminder | cece 若区分 yolo/auto，应是 runtime reminder |
| verification agent | `AgentTool/built-in/verificationAgent.ts:10` | Subagent stable prompt + tool gating | 独立 verification subagent prompt，不是主三层 |
| VerifyPlanExecution | `VerifyPlanExecutionTool.ts:28` | Tool contract + runtime verify reminder | Workflow closure layer |

## 8. 对 SWE-bench 7746 的直接含义

`astropy__astropy-7746` 的失败说明：只把 cece SWE-bench 默认模式改成 plan，并不等于复制了 Claude Code 的完整行为协议。

原因是：

1. **只复制了 runtime planning shell**
   - cece plan reminder 让模型先计划，再 ExitPlanMode。
   - 但它没有引入 Claude Code static Stable 中的 bugfix / verification / faithful reporting 密度。

2. **没有 completion counterweight 的强表达**
   - cece 有 “Don’t gold-plate”，但缺少同等强度的 “don’t leave it half-done”。
   - 7746 正是 half-fix：修住 `np.zeros((0, 2))`，漏掉 `([], [1])`。

3. **没有 bugfix-specific verification workflow**
   - Claude Code verification agent 对 bug fix 的要求是：Reproduce original bug → verify fix → regression tests → related side effects。
   - cece 当前 plan reminder 的 Verification section 是通用 “How to test end-to-end”，不强制“issue 中每个输入形态都要变成复现断言”。

4. **没有 workflow closure 工具**
   - Claude Code 可以在实现后用 verifier 或 VerifyPlanExecution 做第二道门。
   - cece 当前主要靠模型自觉运行测试并汇报。

所以，7746 的教训不是“cece 没有分层”，而是：cece 目前的各层内容还没有形成 Claude Code 那种 Stable contract + runtime mode protocol + verification closure 的组合。

## 9. 本文刻意不做的事

本文不决定 cece 要怎么改，只完成归类：

- 哪些 Claude Code 内容属于 Stable。
- 哪些属于 Session / Dynamic sections。
- 哪些属于 runtime reminders。
- 哪些属于 subagent / tool-level workflow closure。

下一步如果要设计 cece prompt 分工，应基于这张映射表决定：哪些应该进入 cece Stable，哪些进入 Session，哪些进入 Turn，哪些应该保持为 Plan mode reminder 或新建 workflow closure。
