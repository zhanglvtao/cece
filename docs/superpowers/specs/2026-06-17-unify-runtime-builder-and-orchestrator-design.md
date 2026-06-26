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
2. 建立 `AgentRuntimeBuilder + AgentProfile + Orchestrator` 的清晰分层。
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
6. 不做 attach / re-attach。

---

## 3. 核心架构分层

### 3.1 Orchestrator

`Orchestrator` 是多 Agent 控制平面，也是唯一消息总线，负责：

- 接收 `ControlCommand`
- 将需要下发给 Agent 的命令转换为 `AgentCommand`
- 管理 `AgentRuntime` registry
- 跟踪 supervision-level state
- 维护 pending state
- 消费 `AgentEvent`
- 生成 `RenderEvent`
- 做失败隔离、权限校验、取消与状态提交

`Orchestrator` 不负责 turn loop，不持有单 Agent 的会话历史。

### 3.2 AgentRuntimeBuilder

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

### 3.3 AgentRuntime

`AgentRuntime` 是单 Agent 实例对象，主 Agent 与 worker Agent 都是这个对象。

建议它至少包含：

- `Profile`
- `Engine`
- `Mediator`
- `Context / Cancel`
- `SessionID`
- `RuntimeState`
- `Inbox`
- `Outbox`
- 轻量关联字段（如 `ParentSessionID`）

### 3.4 Engine

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
- multi-agent coordination

---

## 4. 启动链路

```text
main
  -> Orchestrator
  -> foreground interactive AgentRuntime
  -> TUI 绑定 ControlCommand / RenderEvent
```

当前范围约束：

- 本轮不做 attach / re-attach
- TUI 生命周期先与当前 cece 进程绑定
- attach 所需的 render snapshot、cursor、attach token、重连协议都延后设计

---

## 5. 统一消息模型

### 5.1 四类消息

1. `ControlCommand`
   - 来自 TUI/用户
   - 先到 `Orchestrator`
2. `AgentCommand`
   - 由 `Orchestrator` 路由后投递到某个 Agent inbox
3. `AgentEvent`
   - AgentRuntime 通过 outbox 发回 `Orchestrator` 的 supervision-level 事件
4. `RenderEvent`
   - `Orchestrator` 基于 control-plane state 与 runtime 可观测数据生成的对外渲染事件

### 5.2 禁止直连

不存在 `Agent -> Agent` 直接消息类型；任何跨 Agent 协作都必须表现为：

```text
AgentEvent -> Orchestrator -> AgentCommand
```

同时：

- `AgentRuntime` 不允许持有其他 runtime / agent 引用
- `Engine` 不暴露跨 agent 控制 API
- `AgentCommand` 只能由 `Orchestrator` 构造并投递
- runtime outbox 只能由 orchestrator 订阅和消费

---

## 6. 架构图

```text
                    ControlCommand                 RenderEvent
TUI  ───────────────────────►  Orchestrator  ───────────────────────►  TUI
                                   │
                                   │ AgentCommand
                                   ▼
                         ┌──────────────────────┐
                         │ foreground AgentRuntime │
                         │ or background AgentRuntime│
                         │----------------------│
                         │ inbox                │
                         │ command loop         │
                         │ Engine               │
                         │ outbox               │
                         └──────────┬───────────┘
                                    │
                                    │ AgentEvent
                                    ▼
                               Orchestrator

Forbidden:
  AgentRuntime A  -X->  AgentRuntime B
  TUI            -X->  AgentRuntime
```

---

## 7. 状态流转与 Pending 模型

### 7.1 AgentStatus

```text
starting
  -> running
  -> waiting_input
  -> waiting_confirm
  -> waiting_plan
  -> completed
  -> failed
  -> cancelling
  -> cancelled
```

约束：

- AgentRuntime 产生执行事实，并通过 `AgentEvent` 上报
- Orchestrator 负责采纳并 commit 状态迁移到 control-plane registry
- `waiting_*` 恢复执行时，必须由新的 `ControlCommand` 进入 `Orchestrator`，再转成 `AgentCommand`
- 不允许 AgentRuntime A 直接唤醒 AgentRuntime B

### 7.2 PendingState

```text
PendingState:
  pending_id
  agent_id
  kind(question | confirm | plan)
  request_id
  message_id
  trace_id
  summary
  payload
  created_at
```

规则：

- 每个 agent 同时最多只有一个 active pending
- 新 pending 到来时，必须显式覆盖或关闭旧 pending，并记录状态流转
- `ControlCommand(answer/confirm/reject)` 必须绑定目标 pending；若请求里未显式带 `pending_id`，则只能在“当前恰好存在一个 active pending”时由 orchestrator 自动绑定
- pending 的创建、消费、拒绝、取消都必须经过 orchestrator commit

---

## 8. 事件分层模型

### 8.1 RuntimeInternalEvent

AgentRuntime / Engine 内部执行事件，不直接进入 orchestrator outbox。

例子：

- tool call 明细
- provider stream delta
- API 原始错误
- token 变化
- thinking/tool exec 细节

### 8.2 AgentEvent

supervision-level 事件，进入 runtime outbox，由 orchestrator 消费。

例子：

- `pending`
- `completed`
- `failed`
- `cancelled`
- 少量 `progress/activity`

### 8.3 RenderEvent

orchestrator 基于 control-plane state 与 runtime 可观测数据生成的对外渲染事件。

例子：

- TUI 状态刷新
- session render
- 结构化日志
- 兼容现有 `SubAgent*Event` 的协议事件

分层规则：

- 不是所有 Agent 内部事件都进入 outbox
- `ask question` 这类需要调度器介入的事件，应提升为 `AgentEvent(pending)`
- API/tool/provider 的原始错误，只有在影响 supervision 状态时才提升为 `AgentEvent(failed)` 或 `AgentEvent(progress)`
- 工具调用细节默认留在 `RuntimeInternalEvent`，必要时只摘要成 `progress/activity`

---

## 9. Mailbox / Backpressure 设计

`inbox/outbox` 是 mailbox 语义，不预设必须由裸 `chan` 实现；第一版可以用内存队列、条件变量、带策略的 channel 包装器，关键是投递语义稳定。

### 9.1 Inbox

- `AgentCommand` 属于可靠控制消息：默认必须可靠入箱，不允许静默丢弃
- `AgentInbox.Enqueue(ctx, cmd)`：可靠投递；满载时由 orchestrator 感知阻塞或收到结构化错误，不能静默丢
- `AgentInbox.Dequeue(ctx)`：由 runtime command loop 串行消费
- `cancel` 属于高优先级控制命令；若 inbox 满载，仍需保证可达，必要时允许单独优先通道或抢占策略

### 9.2 Outbox

`AgentEvent` 必须分级：

- **关键事件**：`pending/completed/failed/cancelled`
- **非关键事件**：`progress/activity`

规则：

- `AgentOutbox.Publish(ctx, ev)`：按事件等级决定投递策略
- 关键事件必须阻塞等待进入 box，直到成功、runtime context 取消、或 orchestrator 关闭订阅
- 非关键事件允许 best-effort：可丢弃、覆盖、合并、降采样，但不能阻塞关键路径
- orchestrator 消费 outbox 时优先处理关键事件，再处理非关键事件

设计约束：

- mailbox 的核心是投递策略，不是具体并发原语
- 若底层使用 channel，也必须由 mailbox 层封装 critical / best-effort 两类语义，不能把所有事件当成同权消息
- 任意实现都必须满足：关键事件不丢，非关键事件允许丢

---

## 10. Agent Tool 的控制面模型

| 外部命令（ControlCommand） | Orchestrator 动作 | 内部命令（AgentCommand） | 说明 |
| --- | --- | --- | --- |
| `start` | 创建并注册新的 runtime | 无 | `start` 是控制面 spawn，不是发给已存在 agent inbox 的命令。 |
| `send` | 校验 agent 存在且可继续执行 | `send_input` | 把新的文本输入投递到目标 agent inbox。 |
| `answer` | 校验当前 pending 为 question | `answer_question` | 把问答结果投递到目标 agent inbox。 |
| `confirm` | 校验当前 pending 为 confirm/plan | `confirm_pending` | 表示用户批准继续。 |
| `reject` | 校验当前 pending 为 confirm/question/plan | `reject_pending` | 表示用户拒绝当前 pending 请求。 |
| `cancel` | 校验 agent 未结束 | `cancel` | 请求目标 agent 进入取消流程。 |
| `status` | 读取 orchestrator registry | 无 | 纯控制面查询，不进入 agent inbox。 |
| `wait` | 等待 orchestrator 持有的 completion/pending 变化 | 无 | 纯控制面等待，不进入 agent inbox。 |

解释：

- `ControlCommand` 是用户/TUI 发给 `Orchestrator` 的外部命令
- `AgentCommand` 是 `Orchestrator` 校验、路由后，才允许投递到某个 Agent inbox 的内部命令
- 不是每个 `ControlCommand` 都会变成 `AgentCommand`：`start/status/wait` 都是控制面动作
- 只有 `send/answer/confirm/reject/cancel` 这类“作用于某个已存在 agent”的命令，才会被翻译成 `AgentCommand`

---

## 11. 可观测性与日志要求

可观测性是本次分层的一级目标，而不是实现阶段的附加打点。

### 11.1 Orchestrator 日志

至少记录：

- spawn 请求
- agent registry 变化
- `status/send/answer/confirm/reject/cancel`
- pending state 迁移
- completion / failure / cancellation
- 非法状态操作

### 11.2 AgentRuntime / Engine 日志

至少记录：

- turn 开始 / 结束
- model request
- tool call / tool result
- question / confirm / plan wait
- runtime state transition
- cancellation / failure
- session persistence 关键动作

### 11.3 日志字段要求

关键日志应尽量带上以下结构化字段中的一部分：

- `agent_id`
- `session_id`
- `parent_session_id`
- `message_id`
- `trace_id`
- `causation_id`
- `operation`
- `status_from`
- `status_to`
- `model`
- `turn_count`
- `tool`
- `error`

---

## 12. 迁移路径

1. Phase 1：显式 inbox/outbox + `ControlCommand/AgentCommand/AgentEvent` 术语落地。
2. Phase 2：统一 envelope 与 trace 字段，兼容旧 `SubAgent*Event`。
3. Phase 3：移除 `Engine.agentInbox` 真相地位，改为 orchestrator render state。
4. Phase 4：把前台 interactive runtime 也纳入同一总线，形成真正 `ControlCommand -> Orchestrator -> AgentRuntime[*] -> AgentEvent -> Orchestrator` 模型。
