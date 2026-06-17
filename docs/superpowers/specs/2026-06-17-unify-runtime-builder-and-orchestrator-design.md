# 统一 AgentRuntimeBuilder 与 Orchestrator 的分层设计

日期：2026-06-17

## 1. 背景

cece 当前已经具备多 Agent 的雏形，但相关能力仍然以 `subagent` 的特殊路径存在，主要问题有：

1. runtime 构建流程重复：
   - 顶层 `runtime.Build(...)`
   - `subAgentFactory.NewSubAgentRuntime(...)`
   - `Engine.startSubAgentBare(...)`
2. `Engine` 同时承担了两类职责：
   - 单 Agent 运行内核
   - 子 Agent 生命周期管理与桥接
3. `subagent` 仍然是架构特例，而不是统一 `AgentRuntime` 模型下的一个 profile。
4. `Agent` tool 当前面向 `Engine` 内部子 Agent 机制，而不是面向独立的编排控制平面。
5. 当前日志对多 Agent 生命周期已有一定覆盖，但缺少以统一 runtime / orchestration 视角组织的结构化可观测性模型。

本设计的目标不是单纯去重，而是建立一个能自然扩展到 multi-agent 的清晰分层。

---

## 2. 目标与非目标

### 2.1 目标

本次设计要实现：

1. 统一单 Agent runtime 的构建流程。
2. 建立 `AgentRuntimeBuilder + AgentProfile + Orchestrator + RuntimeHost` 的清晰分层。
3. 让主 Agent 与 worker Agent 在对象模型上平级，都是 `AgentRuntime` 实例。
4. 将父子关系、生命周期、状态汇总和异步协作从 `Engine` 中迁出，交给独立 `Orchestrator`。
5. 保留 `Agent` tool，作为 LLM 面向 `Orchestrator` 的异步控制面。
6. 提供足够多的结构化日志，确保多 Agent 生命周期与控制面操作可追踪。

### 2.2 非目标

本次设计首版明确不做：

1. 不做复杂 DAG 调度、优先级调度、自动 fan-in 聚合器。
2. 不在首版支持多级递归 worker spawn。
3. 不重做整套 `protocol.Action/Event`，只做必要扩展。
4. 不引入动态 profile 注册平台；profile 首版为仓库内静态定义。
5. 不顺带重构所有 session/store 语义。

---

## 3. 核心架构分层

### 3.1 RuntimeHost

`RuntimeHost` 是进程级根对象，负责：

- 持有共享基础设施
- 持有 `AgentRuntimeBuilder`
- 持有 `Orchestrator`
- 创建并绑定当前前台会话对应的 `interactive` AgentRuntime
- 对 TUI 暴露统一 action/event 入口

TUI 启动后连接的是 `RuntimeHost`，而不是裸 `Engine`。`RuntimeHost` 内部再通过 `Orchestrator` 创建当前 foreground runtime。

### 3.2 Orchestrator

`Orchestrator` 是多 Agent 控制平面，负责：

- spawn / cancel / wait / status / send / answer / confirm / reject
- 管理 `AgentRuntime` registry
- 跟踪 parent/child 或 task 关系
- 汇总 supervision-level state
- 维护 pending state

`Orchestrator` 不负责 turn loop，不持有单 Agent 的会话历史。

### 3.3 AgentRuntimeBuilder

`AgentRuntimeBuilder` 是单 Agent runtime 的统一工厂，负责：

- 选择 model/client
- 构建 registry
- 应用 tool filtering policy
- 构建 prompt assembler
- 创建 `Engine`
- 创建 `Mediator`
- 注入 context/cancel/store 等依赖
- 组装 `AgentRuntime`

它只负责装配，不负责调度与 supervision。

### 3.4 AgentRuntime

`AgentRuntime` 是单 Agent 实例对象，主 Agent 与 worker Agent 都是这个对象。

建议它至少包含：

- `Profile`
- `Engine`
- `Mediator`
- `Context / Cancel`
- `SessionID`
- `RuntimeState`
- `EventStream`
- 轻量关联字段（如 `ParentRef` / `TaskRef`）

### 3.5 Engine

`Engine` 收缩为单 Agent 内核，只负责：

- conversation state
- turn loop host
- event emission
- history/session/token/tool stats
- queued user input
- confirm/question/plan 交互

`Engine` 不再负责：

- `subAgents` registry
- child runtime 创建
- parent/child supervision
- bridge child events
- multi-agent coordination

---

## 4. 对象模型

建议稳定成以下 5 个核心对象：

1. `RuntimeHost`
2. `Orchestrator`
3. `AgentRuntimeBuilder`
4. `AgentRuntime`
5. `AgentProfile`

创建关系如下：

```text
RuntimeHost
 ├── owns SharedInfra
 ├── owns AgentRuntimeBuilder
 ├── owns Orchestrator
 └── asks Orchestrator to create foreground interactive AgentRuntime

Orchestrator
 ├── uses AgentRuntimeBuilder
 ├── creates AgentRuntime
 ├── stores AgentHandle / AgentRef
 └── supervises runtime lifecycle
```

这意味着：

- `RuntimeHost` 创建 `Orchestrator`
- `Orchestrator` 使用 `AgentRuntimeBuilder`
- `AgentRuntimeBuilder` 创建 `AgentRuntime`
- `AgentRuntime` 内部持有 `Engine`

---

## 5. 启动与主数据流

### 5.1 TUI 启动路径

TUI 启动后连接 `RuntimeHost`。在 host 内部：

1. 初始化 shared infra
2. 创建 `Orchestrator`
3. 请求 `Orchestrator` 创建前台 `interactive` runtime
4. 将该 runtime 绑定为 foreground session
5. 对 TUI 发出 ready/session 事件

关键意义：

- 顶层不再直接 new `Engine`
- interactive agent 从第一天起就是一个普通 `AgentRuntime`
- worker agent 只是另一个 `AgentRuntime`

### 5.2 普通消息的数据流

```text
TUI Input
  -> RuntimeHost
  -> current foreground AgentRuntime
  -> Engine.Input(...)
  -> TurnLoop
  -> protocol events
  -> RuntimeHost
  -> TUI
```

这里 `Orchestrator` 不介入普通单 Agent turn。

### 5.3 Agent.start 的数据流

```text
interactive AgentRuntime
  -> Agent tool
  -> Orchestrator.spawn(profile=worker, ...)
  -> AgentRuntimeBuilder.Build(...)
  -> worker AgentRuntime created
  -> agent_id returned immediately
  -> worker runtime runs asynchronously
  -> Orchestrator tracks lifecycle and pending state
```

`Agent` tool 不再直接操作 `Engine` 的子 Agent 特例，而是变成 LLM 面向 `Orchestrator` 的控制面。

---

## 6. AgentProfile / Policy 设计

`AgentProfile` 只描述单 Agent 行为差异，不描述多 Agent 关系，也不持有共享基础设施句柄。

首版 profile 框架允许扩展，但仓库内先落地：

- `interactive`
- `worker`

### 6.1 应进入 profile 的策略

1. `PromptPolicy`
   - stable prompt 模板
   - session context 范围
   - role-level instructions

2. `ToolPolicy`
   - 可见工具集合
   - 是否允许 `Agent`
   - 是否允许 write/bash/MCP 等能力

3. `InteractionPolicy`
   - 是否允许直接问用户
   - question/confirm/plan 的处理方式

4. `ResultPolicy`
   - 返回 summary 还是 artifact-first
   - preview / truncation / result packaging

5. `EffortPolicy`
   - reasoning effort
   - 默认 max turns
   - token budget 倾向

6. `SpawnPolicy`
   - 是否允许 spawn agents
   - 可 spawn profile 白名单

### 6.2 不应进入 profile 的内容

1. parent/child 关系本身
2. foreground/background UI 归属
3. agent registry / lifecycle bookkeeping
4. store / MCP manager / provider resolver / logger 等 infra handles

### 6.3 首版两个 profile 的建议

#### interactive

- 完整交互能力
- 可见 TUI-facing question/confirm/plan 流
- 允许 `Agent` tool
- 结果直接面向用户
- 可 spawn `worker`

#### worker

- 默认不面向用户直接交互
- question/confirm/plan 通过 `Orchestrator` 暴露为 pending state
- 首版默认禁用 `Agent`
- 结果以 parent-consumable summary / artifact 为主
- 默认 low effort
- 默认受 max turns 限制

---

## 7. Agent Tool 的异步控制模型

`Agent` tool 保留，并被定义为：

> LLM 面向 `Orchestrator` 的异步控制 API

保留以下操作：

- `start`
- `status`
- `send`
- `answer`
- `confirm`
- `reject`
- `cancel`

### 7.1 start

`start` 只负责 spawn，不等待最终结果。它返回：

- `agent_id`
- 初始状态
- 简要摘要

### 7.2 status

返回结构化快照，至少包含：

- `agent_id`
- `profile`
- `status`
- `session_id`
- `last_activity`
- `last_message`
- `last_tool`
- `turn_count`
- token 统计
- `pending`

### 7.3 send / answer / confirm / reject / cancel

这些操作都返回：

- 动作确认（ack）
- 当前最新状态

### 7.4 PendingState

`Orchestrator` 应统一维护：

```text
PendingState
- kind: none | question | confirm | plan
- agent_id
- request_id
- summary
- payload
```

这样 parent agent 和 TUI 都能稳定理解子 Agent 当前卡在什么状态。

---

## 8. 迁移路径

建议按以下顺序推进。

### Phase 1：抽出统一的 AgentRuntimeBuilder

统一以下三套构建逻辑：

- 顶层 `runtime.Build(...)`
- `subAgentFactory.NewSubAgentRuntime(...)`
- `Engine.startSubAgentBare(...)`

先统一构建流程，不立即改 supervision。

### Phase 2：显式引入 AgentProfile

将目前写死在 subagent 路径中的差异沉淀为 profile/policy：

- prompt 差异
- tool 差异
- low effort
- artifact/result policy

### Phase 3：把 subagent 管理从 Engine 搬到 Orchestrator

从 `Engine` 迁出：

- `subAgents`
- `nextSubAgentID`
- `SubAgentRuntimeFactory`
- `start/status/send/answer/confirm/reject/cancel` 等子 Agent 管理逻辑
- `bridgeSubRuntimeEvents(...)`

迁入 `Orchestrator` 与其 supervision/observer 机制。

### Phase 4：让 Agent tool 面向 Orchestrator

保持 tool schema 基本稳定，先替换其后端路由：

- `start` -> `Orchestrator.Spawn(...)`
- `status` -> `Orchestrator.Status(...)`
- `send` -> `Orchestrator.Send(...)`
- `answer` -> `Orchestrator.Answer(...)`
- `confirm` -> `Orchestrator.Confirm(...)`
- `reject` -> `Orchestrator.Reject(...)`
- `cancel` -> `Orchestrator.Cancel(...)`

### Phase 5：引入 RuntimeHost

将 TUI 连接对象从 “裸 engine/mediator” 升级为 `RuntimeHost`，并由 host 创建前台 `interactive` runtime。

---

## 9. 可观测性与日志要求

可观测性是本次分层的一级目标，而不是实现阶段的附加打点。

### 9.1 总体要求

`RuntimeHost`、`Orchestrator`、`AgentRuntimeBuilder`、`AgentRuntime`、`Engine` 五层都必须提供结构化日志。任一 agent 的创建、状态迁移、控制面操作、关键执行步骤和失败路径，都应能通过日志完整追踪。

### 9.2 RuntimeHost 日志

至少记录：

- host 启动 / 关闭
- shared infra 初始化结果
- foreground runtime 创建 / 切换 / 恢复
- TUI action 的路由目标
- host-level 错误

### 9.3 Orchestrator 日志

至少记录：

- spawn 请求
- build request / profile 选择
- agent registry 变化
- parent/child relation 建立
- `status/send/answer/confirm/reject/cancel`
- pending state 迁移
- completion / failure / cancellation
- 非法状态操作

### 9.4 AgentRuntimeBuilder 日志

至少记录：

- build 开始 / 完成 / 失败
- profile 名称
- model / context window / max turns
- tool policy 结果
- prompt policy 结果
- mediator 是否启用
- runtime id / session id 关联信息

### 9.5 AgentRuntime / Engine 日志

至少记录：

- turn 开始 / 结束
- model request
- tool call / tool result
- question / confirm / plan wait
- runtime state transition
- cancellation / failure
- session persistence 关键动作

### 9.6 日志字段要求

关键日志应尽量带上以下结构化字段中的一部分：

- `agent_id`
- `profile`
- `session_id`
- `parent_agent_id`
- `request_id`
- `operation`
- `status_from`
- `status_to`
- `model`
- `turn_count`
- `tool`
- `error`

### 9.7 三类日志视角

建议明确区分：

1. lifecycle logs
2. control plane logs
3. execution logs

这样可以快速判断问题属于 orchestration、single-agent execution，还是 host-level routing。

---

## 10. 错误处理

### 10.1 Builder 层错误

例如：

- model/client 无法解析
- profile 不合法
- tool policy 冲突
- prompt policy 构建失败

处理原则：构建失败，不进入运行态，不创建半初始化 runtime。

### 10.2 Runtime 层错误

例如：

- turn loop 失败
- provider stream 失败
- tool 执行失败
- session 持久化失败
- context cancelled

处理原则：运行错误留在 runtime，通过 protocol event 可观测；`Orchestrator` 只消费 supervision-level state。

### 10.3 Orchestrator 层错误

例如：

- spawn 失败
- `agent_id` 不存在
- 对已完成 agent 调用 send/answer/confirm/reject
- invalid state transition

处理原则：作为控制面结构化错误返回，明确区分：

- `not_found`
- `already_finished`
- `invalid_state`
- `spawn_denied`
- `build_failed`

### 10.4 Host 层错误

例如：

- foreground runtime 不存在
- session 恢复失败
- shared infra 初始化失败
- TUI 请求路由失败

处理原则：作为 host-level error 暴露，不伪装成 runtime 内部错误。

---

## 11. 测试策略

### 11.1 Builder 测试

覆盖：

- interactive / worker profile 产物差异
- registry / prompt / effort / spawn policy 差异
- invalid build request/profile 的失败路径

### 11.2 Engine 测试

覆盖：

- 单 Agent turn loop
- tool execution / question / plan / confirm
- queued input
- compact / prune / trim
- token/session stats

重点：Engine 的测试对象只剩“单 Agent 怎么跑”。

### 11.3 Orchestrator 测试

覆盖：

- spawn runtime
- status/send/answer/confirm/reject/cancel
- pending state 流转
- completed/failed/cancelled 状态迁移
- parent/child relation
- finished agent 的非法操作错误

### 11.4 Host 测试

覆盖：

- startup 创建 foreground interactive runtime
- load session / new session
- action route 到当前 runtime
- host-level error propagation

### 11.5 端到端测试

至少覆盖：

1. 普通 interactive 聊天
2. `Agent.start` 创建 worker
3. `Agent.status`
4. worker 提问，parent 用 `Agent.answer`
5. worker 请求确认，parent 用 `Agent.confirm/reject`
6. worker 完成并返回 summary / artifact
7. cancel running worker
8. load / resume session

---

## 12. 验收标准

本次重构应满足以下可检验标准：

1. 单 Agent 与多 Agent 责任彻底分离：
   - 单 Agent 看 `Engine`
   - 多 Agent 看 `Orchestrator`
2. 顶层与 worker 走同一个 builder pipeline。
3. interactive 与 worker 的差异通过 profile 表达，而不是散落的 `if subagent` 分支。
4. `Agent` tool 成为纯控制面，不再直接操纵 `Engine` 内部 child map。
5. TUI 连接的是 `RuntimeHost`，不是裸 engine。
6. 五层对象都具备结构化日志，任一 agent 生命周期都可追踪。

---

## 13. 首版范围结论

首版统一 runtime 分层的落地边界如下：

- 提供通用 `AgentRuntimeBuilder`
- 提供通用 `AgentProfile` 框架
- 仓库内仅实现 `interactive` / `worker`
- 引入独立 `Orchestrator`
- `Agent` tool 面向 `Orchestrator` 的异步控制面
- 引入 `RuntimeHost` 作为 TUI 连接的进程级根对象

worker 在首版默认不允许 spawn child agent，复杂 fan-out/fan-in 调度不进入本轮范围。

---

## 14. 设计总结

本设计的本质不是“给 subagent 换个名字”，而是把 cece 从“单 Agent + 特殊 subagent 路径”升级为“统一 AgentRuntime 模型 + profile + orchestrator”的可扩展结构。

如果这次分层成功，未来新增 reviewer / planner / background agent 时，不需要继续引入新的 runtime 特例，而只需要：

1. 增加 profile
2. 增加少量 orchestration policy
3. 复用同一 builder 与 runtime 对象模型

这样 multi-agent 就会成为平台能力，而不是持续叠加的架构例外。
