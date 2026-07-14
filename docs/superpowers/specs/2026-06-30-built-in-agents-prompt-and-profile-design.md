# Built-in Agents：Prompt 心智同步与轻量行为差异设计

## Context

当前项目已经把子 agent 从单一 `worker` 拆成了 4 个真实任务型 profiles：`research`、`coding`、`review`、`execution`。同时，`Agent` tool 也已经显式暴露这些 built-in agents，并要求 `operation=start` 时必须传 `agent_type`。

但当前仍有两个明显缺口：

1. **主 agent 的调用心智不足**
   - 虽然 `Agent` tool description 已经说明有 built-in agents，但主系统 prompt 还没有系统性地教模型：什么时候该起 agent、4 个 built-in agents 分别适合什么任务。
2. **4 个 built-in agents 的行为差异仍然偏弱**
   - 目前真实 profile 已经存在，默认 effort / max_turns 也已区分，但 prompt 层还没有按 profile 注入职责边界，因此 `research/coding/review/execution` 的行为差异主要还停留在名字和默认值层面。

本设计要在**同一轮**里解决这两个问题，但保持范围克制：

- 同时增强主 agent 的 built-in agents 调用心智；
- 同时增强 4 个 built-in agents 的轻量行为差异；
- 不把 4 个 profiles 做成 4 套完全独立的系统；
- 不在本轮引入重型工具权限切分。

## Goals

1. 让前台主 agent 更稳定地识别何时应该调用 `Agent` tool。
2. 让主 agent 明确知道 4 个 built-in agents 的职责边界：
   - `research`
   - `coding`
   - `review`
   - `execution`
3. 让被启动出来的 built-in agents 在 prompt 层体现轻量但真实的行为差异。
4. 保持 prompt 结构可维护，避免复制 4 份完整系统 prompt。

## Non-goals

- 不做 4 个 profiles 的重型工具权限隔离。
- 不引入用户自定义 agent profiles。
- 不重写整份 stable system prompt 的整体结构。
- 不修改 observatory / UI 展示模型。
- 不在本轮开放子 agent 再次调用 `Agent` 生成孙 agent。

## Existing Structure

当前 prompt 结构入口：

- `internal/prompt/system.go:14`
  - `FormatStableSystemPrompt(repoRoot string)` 返回共享稳定系统 prompt。
- `internal/prompt/system.go:20`
  - `FormatSubAgentSystemPrompt(repoRoot string, systemPromptExtra string)` 当前只是把 stable prompt 与额外指令拼接。
- `internal/prompt/system.md:1`
  - 当前共享稳定系统 prompt 内容。

当前问题在于：

- interactive 主 agent 与 built-in agents 共享同一份基础 prompt，但没有显式的 built-in agents 使用策略层；
- sub-agent prompt 没有按 profile 注入角色差异；
- 因此 profile 的“真实存在”更多体现在 runtime，而不是模型可感知的 prompt 行为。

## Approaches Considered

### 方案 A：只增强主 agent prompt

只在 interactive 主 agent 的系统 prompt 中加入 built-in agents 使用策略，不改 sub-agent prompt。

**优点**
- 改动最小。
- 风险最低。

**缺点**
- 只能提升“会不会调用”，不能明显提升“调用出来的 agent 像不像对应类型”。
- `research/coding/review/execution` 的差异仍然偏弱。

### 方案 B：联合轻量方案（推荐）

- 在主 agent prompt 中加入 built-in agents 使用策略；
- 在 sub-agent prompt 中按 profile 注入轻量职责片段；
- 保持共享基础 prompt，不复制 4 份完整系统。

**优点**
- 同时提升调用概率与角色差异。
- 改动集中，维护成本低。
- 与当前“真实 profile 已存在”的架构自然对齐。

**缺点**
- 差异仍然是轻量差异，不是强隔离。

### 方案 C：强差异方案

为 4 个 built-in agents 分别设计更强的 prompt、工具权限和工作流约束。

**优点**
- 差异最明显。

**缺点**
- 改动面大，容易过度设计。
- 维护成本高，不符合本轮“轻量差异”的范围。

## Decision

采用 **方案 B：联合轻量方案**。

核心原则：

- **一份共享基础 prompt**：继续复用 stable system prompt 作为共同底座。
- **interactive 增加调用策略层**：只给主 agent 增加 built-in agents 使用心智。
- **sub-agent 增加 profile 片段层**：按 `research/coding/review/execution` 注入轻量职责差异。
- **不复制整份 prompt**：避免 4 套 prompt 漂移和维护成本失控。

## Design

### 1. Prompt 分层模型

目标结构：

1. **Stable base prompt**
   - 继续使用 `internal/prompt/system.md` 作为共享基础。
2. **Interactive built-in agents guidance**
   - 仅对前台主 agent 注入一段“何时使用 Agent + 4 个 built-in agents 如何选择”的策略说明。
3. **Sub-agent profile guidance**
   - 仅对 built-in agents 注入一段按 profile 区分的职责片段。
4. **Call-site extra instructions**
   - 保留现有 `systemPromptExtra` 作为调用时附加说明。

这样可以保证：

- 主 agent 学会“怎么选人”；
- 子 agent 学会“按什么角色做事”；
- 共享基础 prompt 不被复制。

### 2. 主 agent 的 built-in agents 使用策略

interactive 主 agent 需要新增一段明确策略，内容包括：

- 遇到以下场景时优先考虑 `Agent`：
  - 独立子任务
  - 可并行任务
  - 耗时任务
  - 需要后台推进或等待的任务
- 4 个 built-in agents 的职责边界：
  - `research`：搜索、阅读、归纳、调研
  - `coding`：实现、修复、补测试、改代码
  - `review`：检查、审查、验证、风险判断
  - `execution`：推进、等待、跟进、状态回传
- 强调先判断任务类型，再决定是否起 agent。
- 强调 `Agent(start)` 时必须显式指定 `agent_type`。

这段策略不应该写成冗长教程，而应该是简洁、可执行的决策规则。

### 3. 4 个 built-in agents 的轻量职责片段

每个 built-in agent 在共享基础 prompt 之上追加一小段 profile guidance。

#### research
- 目标：搜索、阅读、归纳、形成现状理解。
- 行为倾向：先收集证据，再总结结论。
- 边界：默认不把重点放在直接改代码，除非任务明确要求。

#### coding
- 目标：实现功能、修复问题、补必要测试。
- 行为倾向：直接推进实现，保持改动聚焦。
- 边界：避免把大量时间花在开放式调研上。

#### review
- 目标：检查方案、审查改动、验证风险与遗漏。
- 行为倾向：偏审阅、质疑、验收。
- 边界：默认不承担大规模实现任务，除非任务明确要求。

#### execution
- 目标：推进流程、等待结果、持续跟进、汇报状态。
- 行为倾向：偏执行与推进，而不是开放式分析。
- 边界：默认不承担深度设计或大规模实现。

这些差异是“轻量差异”：
- 主要影响模型的工作方式与注意力分配；
- 不通过重型工具权限隔离来强制实现。

### 4. 与 runtime profile 的关系

prompt 层设计必须与现有 runtime profile 对齐，而不是另起一套概念。

当前 runtime 已有：
- `research`
- `coding`
- `review`
- `execution`

prompt 层只负责把这些真实 profile 的职责边界显式化，让模型能感知并遵循；
runtime 继续负责默认 effort / max_turns / spawn policy 等执行策略。

也就是说：
- runtime profile 决定“系统怎么跑”；
- prompt guidance 决定“模型怎么想”。

### 5. API / 组装接口建议

为了避免 prompt 逻辑散落，建议把 prompt 组装接口显式分层：

- 保留 `FormatStableSystemPrompt(...)`
- 为 interactive 增加一个明确的 guidance 组装入口
- 为 sub-agent 增加按 profile 选择 guidance 的组装入口

推荐方向：

- `FormatInteractiveSystemPrompt(...)`
  - 返回 stable base + interactive built-in agents guidance
- `FormatSubAgentSystemPrompt(..., profile, systemPromptExtra)`
  - 返回 stable base + profile guidance + extra instructions

重点不是函数名本身，而是：
- interactive guidance 与 sub-agent guidance 分开；
- profile 选择逻辑集中在 prompt 层，不散落在 runtime / tool / engine 多处。

### 6. 测试策略

本轮测试重点放在 prompt 组装正确性，而不是做大规模行为测试。

至少覆盖：

1. **interactive prompt 测试**
   - 断言 built-in agents 使用策略被注入；
   - 断言包含 `research/coding/review/execution` 与使用场景。

2. **sub-agent prompt 测试**
   - 断言不同 profile 会注入不同 guidance 片段；
   - 断言共享 stable base 仍然存在；
   - 断言 `systemPromptExtra` 仍然会被追加。

3. **兼容性测试**
   - 保持现有 runtime/tool 测试继续通过；
   - 确保 prompt 组装改动不会破坏现有调用链。

## File Impact

预计主要影响：

- `internal/prompt/system.go`
  - 增加 interactive / sub-agent guidance 组装逻辑。
- `internal/prompt/system.md`
  - 视实现方式决定是否补充一小段 built-in agents guidance，或保持纯 base prompt。
- 可能新增或内嵌 profile guidance 常量
  - 位置应尽量靠近 `internal/prompt/system.go`，避免散落。
- `internal/prompt/*_test.go`
  - 增加 prompt 组装测试。
- 如有调用方需要切换到新的 prompt 组装入口，则同步调整相关 runtime/builder 调用点。

## Risks

1. **prompt 过长**
   - 如果 interactive guidance 和 profile guidance 写得太长，会增加 token 成本并稀释重点。
   - 解决方式：保持 guidance 简洁、规则化。

2. **概念重复**
   - 如果 tool description、runtime profile、prompt guidance 三处说法不一致，会让模型心智混乱。
   - 解决方式：统一使用 built-in agents / agent_type / research/coding/review/execution 这套术语。

3. **差异过弱**
   - 如果 profile guidance 太轻，模型行为差异可能仍不明显。
   - 解决方式：先做轻量差异，但确保每个 profile 都有明确目标、行为倾向和边界。

4. **差异过强**
   - 如果 guidance 写得过于刚性，可能让 agent 在边界任务上表现僵硬。
   - 解决方式：强调“默认倾向”，而不是绝对禁止。

## Verification

实现后至少验证：

- prompt 组装测试通过；
- `go test ./internal/prompt ./internal/runtime ./internal/tool ./internal/engine/...` 通过；
- interactive prompt 中能看到 built-in agents 使用策略；
- sub-agent prompt 中能看到对应 profile 的 guidance 片段。

## Open Decisions Resolved

本设计已明确以下决策：

- 两个方向同一轮一起做，而不是拆成两轮。
- 主 agent 与子 agent 两边都教。
- 4 个 built-in agents 只做轻量差异，不做重型工具权限切分。
- 差异主要落在 prompt 片段与默认执行策略，而不是 4 套完全独立系统。

## Summary

本设计的核心是：

- 用 **interactive guidance** 提升主 agent 的 built-in agents 调用心智；
- 用 **profile guidance** 提升 `research/coding/review/execution` 的轻量行为差异；
- 用 **共享基础 prompt + 小片段追加** 的方式保持复用、解耦和可维护性。

这能在不扩大到重型权限系统的前提下，同时提升 Agent tool 调用概率与 built-in agents 的角色清晰度。