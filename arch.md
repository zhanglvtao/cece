# cece 架构文档

## 项目定位

生产级 code agent CLI，多协议 LLM 后端，Go 实现，从零构建中学习 agent 架构全部细节。

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 语言 | Go | 编译型单二进制，无运行时依赖，并发原生支持 |
| LLM 协议 | Anthropic + Aiden + Codebase | 多协议适配，`ModelClient` 接口统一抽象 |
| 交互模式 | Bubble Tea TUI | 声明式终端 UI，事件驱动，流式友好 |
| 工具体系 | 最小集 + Effect 分级 | Read/Write/Edit/Bash/Grep/Glob + AskUser/PlanMode/Skill |
| Agent 循环 | ReAct + 并行工具调用 | `TurnRunner` 驱动循环，`ToolExecutor` 并行执行 |
| 流式输出 | SSE Token 级流式 | `ModelStreamer` 拆解 stream 事件，实时推送 UI |
| 权限模型 | 三级模式（Default/AutoAccept/Plan） | `InteractionGate` 拦截写操作，Plan 模式只读 |
| 上下文管理 | 三层 Prompt + Token Budget | Stable/Session/Turn 分层，Budget 自动截断 |
| Prompt Caching | Anthropic ephemeral 标记 | Stable/Session 层缓存，Turn 层不缓存 |
| 项目结构 | Go internal 包，接口边界组织 | 严格接口，包间零循环依赖 |
| 终端 UI | Bubble Tea v2 + Lipgloss v2 | 声明式渲染，protocol.Event 驱动更新 |
| 配置管理 | `.cece/settings.json` + 环境变量 | 文件优先，`ANTHROPIC_API_KEY` 等环境变量兜底 |
| 会话持久化 | JSONL 文件存储 | `session.Store` 接口，每条消息 append-only |
| 工具调用 | 原生 tool_use（Anthropic API） | 流式增量 JSON 组装，`toolCallState` 追踪 |
| System Prompt | 三层组装 + 预算截断 | `ContextAssembler` 组装，`enforceBudget` 保底 |
| 错误处理 | 输出截断自动升级重试 | `max_tokens` 截断时静默升级到 64K 重试 |
| Token 计数 | 启发式 + tiktoken 精确 | 70% 阈值以下启发式，超过用 tiktoken |
| 构建工具 | Go build | 单二进制，`cmd/cece/main.go` 入口 |
| 日志 | `log/slog` 结构化日志 | 写入 `.cece/cece.log`，`--debug` 控制 |
| 技能系统 | SKILL.md 文件发现 | 内置 + 项目级，`/skill` 斜杠命令加载 |

## 项目结构

```
cece/
├── cmd/cece/
│   └── main.go              # CLI 入口：配置加载 → Client 创建 → Engine 组装 → TUI 启动
├── internal/
│   ├── chat/                # Agent 核心：循环、消息、事件
│   │   ├── message.go       # Message, ApiContentBlock, ModelClient 接口
│   │   ├── events.go        # UI 事件类型（密封接口 Event）
│   │   ├── adapter.go       # 内部 Event → protocol.Event DTO 转换
│   │   ├── turn_engine.go   # TurnEngine 接口（Engine 实现之）
│   │   ├── turn_bootstrap.go# Turn 启动器：组装 prompt、创建 Runner 依赖
│   │   ├── turn_runner.go   # 核心 ReAct 循环：Stream → Gate → Execute → 循环
│   │   ├── model_streamer.go# 流式调用 LLM，拆解 SSE → modelResponse
│   │   ├── tool_executor.go # 并行工具执行 + 结果截断 + Plan 模式权限
│   │   ├── interaction_gate.go# 用户交互拦截：确认、审批、问答
│   │   ├── session_coordinator.go# 会话生命周期：创建、持久化、元数据更新
│   │   └── tokens.go        # Token 估算（启发式 + tiktoken）
│   ├── engine/              # 引擎层：状态管理、Action 分发
│   │   ├── engine.go        # Engine：历史管理、Input/Confirm/Cancel、事件桥接
│   │   └── mediator.go      # EngineMediator：B-class Action（模型切换/会话加载/模式切换）
│   ├── protocol/            # UI ↔ Runtime 通信协议（纯 DTO，零内部依赖）
│   │   ├── action.go        # Action 密封接口：Input/Confirm/Cancel/SwitchModel...
│   │   ├── event.go         # Event 密封接口：SessionCreated/AssistantDelta/ToolCall...
│   │   └── types.go         # 共享值类型：Message, ContentBlock, ModelInfo, Question...
│   ├── tool/                # 工具系统
│   │   ├── tool.go          # Tool 接口、Definition、Result、Emitter、Effect
│   │   ├── registry.go      # 工具注册中心：注册、查询、执行、输入校验
│   │   ├── permission.go    # 权限检查：autoApproved 白名单
│   │   ├── plan_mode.go     # Plan 模式：PlanModeState、EnterPlanMode/ExitPlanMode 工具
│   │   ├── ask_user.go      # AskUserQuestion 工具
│   │   ├── skill.go         # Skill 工具：加载技能指令
│   │   ├── read.go          # Read 工具
│   │   ├── write.go         # Write 工具
│   │   ├── edit.go          # Edit 工具（精确字符串替换）
│   │   ├── diff.go          # Diff 工具
│   │   ├── bash.go          # Bash 工具
│   │   ├── grep.go          # Grep 工具
│   │   └── glob.go          # Glob 工具
│   ├── prompt/              # System Prompt 组装
│   │   ├── prompt.go        # ContextLayer、PromptSegment、AssembleResult
│   │   ├── assembler.go     # ContextAssembler：三层组装 + 预算截断
│   │   ├── context.go       # SessionContext、TurnContext、SessionCollector 接口
│   │   ├── collector.go     # DefaultSessionCollector：环境/项目指令/工具描述
│   │   ├── render.go        # 格式化：SessionContext/TurnContext → 文本/XML
│   │   ├── budget.go        # Token 预算：分层分配、两阶段估算、截断策略
│   │   ├── tokens.go        # tiktoken 精确计数
│   │   ├── instruction.go   # CLAUDE.md 项目指令加载
│   │   └── system.go        # 嵌入式 system.md + SYSTEM.md 覆盖
│   ├── claude/              # Anthropic 协议客户端
│   │   ├── client.go        # Claude Client：构建请求、发送 SSE
│   │   ├── stream.go        # SSE 流解析：事件拆解为 ApiStreamEvent
│   │   ├── auth_helper.go   # AuthHelper：shell 命令获取动态 token
│   │   └── model_info.go    # /v1/models API：查询可用模型
│   ├── aiden/               # Aiden 协议客户端
│   │   ├── client.go        # Aiden Client：适配不同 API 格式
│   │   ├── stream.go        # Aiden SSE 流解析
│   │   ├── convert.go       # 消息格式转换
│   │   └── serialize.go     # 请求序列化
│   ├── codebase/            # Codebase 协议客户端
│   │   ├── client.go        # Codebase Client：带 config_name 的 API
│   │   ├── stream.go        # Codebase SSE 流解析
│   │   └── serialize.go     # 请求序列化
│   ├── session/             # 会话持久化
│   │   ├── session.go       # Session、SessionMeta 数据结构
│   │   ├── store.go         # Store 接口：Create/Append/Load/List/Delete/UpdateMeta
│   │   ├── filestore.go     # 文件系统实现：JSONL + JSON 元数据
│   │   └── title.go         # 会话标题生成与更新
│   ├── skill/               # 技能系统
│   │   ├── skill.go         # Skill 结构、Validate、FormatListing/FormatInvocation
│   │   ├── discover.go      # 技能发现：扫描 builtin + 项目目录
│   │   ├── parse.go         # SKILL.md 解析
│   │   ├── store.go         # SkillStore：加载、查询、列表
│   │   └── embed.go         # 内置技能嵌入
│   ├── config/              # 配置管理
│   │   └── config.go        # Config 结构、settings.json 加载、环境变量兜底
│   ├── auth/                # 认证辅助
│   │   └── token_cache.go   # TokenCache：shell 命令获取动态 token + TTL 缓存
│   ├── httpretry/           # HTTP 重试
│   │   └── retry.go         # 指数退避重试 RoundTripper
│   ├── logger/              # 日志
│   │   ├── logger.go        # slog 初始化，文件输出
│   │   └── human_handler.go # 人类可读日志格式
│   └── ui/                  # 终端 UI
│       ├── model.go         # Bubble Tea Model：事件消费、状态管理
│       ├── chat.go          # 聊天视图渲染
│       ├── input.go         # 输入框
│       ├── transcript.go    # 对话转录渲染
│       ├── markdown.go      # Markdown 渲染
│       ├── streaming_markdown.go # 流式 Markdown 渲染
│       ├── diff.go          # Diff 视图
│       ├── detail.go        # 详情面板
│       ├── modal.go         # 模态框
│       ├── statusbar.go     # 状态栏
│       ├── slash.go         # 斜杠命令处理
│       ├── slash_popup.go   # 斜杠命令弹出框
│       ├── keys.go          # 快捷键绑定
│       ├── styles.go        # 样式定义
│       ├── helpers.go       # 辅助函数
│       ├── tool_args.go     # 工具参数显示
│       ├── anim/            # 动画
│       ├── dialog/          # 对话框
│       ├── inputqueue/      # 输入队列
│       ├── list/            # 列表
│       └── theme/           # 主题
├── .cece/
│   └── settings.json        # 项目配置（providers、model、permissions）
├── plans/                   # Plan 模式的计划文件目录
├── go.mod
└── go.sum
```

## 核心架构

### 数据流：用户输入 → 模型响应

```
User Input
  │
  ▼
UI (Bubble Tea) ──InputAction──▶ EngineMediator.Do()
  │                                    │
  │                              Engine.Input()
  │                                    │
  │                              ┌─────▼──────┐
  │                              │ TurnBootstrap │
  │                              │  assemble    │
  │                              │  system prompt│
  │                              └─────┬──────┘
  │                                    │
  │                              ┌─────▼──────┐
  │                              │ TurnRunner   │◄─── ReAct 循环
  │                              │  Run()       │
  │                              └──┬─────┬────┘
  │                                 │     │
  │                    ┌────────────▼┐  ┌──▼───────────┐
  │                    │ModelStreamer │  │InteractionGate│
  │                    │ Stream()     │  │ WaitIfNeeded()│
  │                    └──────┬──────┘  └──────┬───────┘
  │                           │                │
  │                    ┌──────▼──────┐  ┌──────▼───────┐
  │                    │ModelClient   │  │ToolExecutor   │
  │                    │ (claude/     │  │ ExecuteBatch() │
  │                    │  aiden/      │  └──────┬───────┘
  │                    │  codebase)   │         │
  │                    └──────┬──────┘  ┌──────▼───────┐
  │                           │         │tool.Registry  │
  │                    SSE Stream      │ Execute()      │
  │                           │         └──────────────┘
  ▼                           ▼
UI ◀── protocol.Event ◀── Event Bus (chan)
```

### 三层 Prompt 架构

```
┌─────────────────────────────────────────────┐
│ Stable Layer（整个会话不变，可缓存）          │
│   - system.md 嵌入式系统提示                  │
│   - 或项目根目录 SYSTEM.md 完整覆盖           │
├─────────────────────────────────────────────┤
│ Session Layer（会话内相对稳定，可缓存）       │
│   - 环境信息（OS, git branch, repo root）     │
│   - CLAUDE.md 项目指令                        │
│   - 工具描述摘要                              │
│   - 技能列表                                  │
├─────────────────────────────────────────────┤
│ Turn Layer（每轮重算，不缓存）               │
│   - 当前时间（关键词触发）                     │
│   - 工作目录                                  │
│   - 对话轮次编号                              │
└─────────────────────────────────────────────┘
```

### 权限三模式

| 模式 | 读工具 | 写工具 | 执行工具 | Plan 文件写入 |
|------|--------|--------|----------|--------------|
| Default | 自动 | 需确认 | 需确认 | — |
| AutoAccept | 自动 | 自动 | 自动 | — |
| Plan | 自动 | 拒绝 | 拒绝 | 自动 |

### Token 预算分配

```
Context Window (e.g. 200K)
├── System Prompt Budget (25% = 50K)
│   ├── Stable Layer (15% of budget = 7.5K)
│   ├── Session Layer (55% of budget = 27.5K)
│   ├── Turn Layer (10% of budget = 5K)
│   └── Reserve (20% of budget = 10K)
└── Messages + Tools (75% = 150K)
```

截断优先级：Turn（最先丢弃）→ Session（剥离工具描述）→ Stable（永不截断）

## 核心接口

### ModelClient（LLM 统一接口）
```go
type ModelClient interface {
    Stream(ctx context.Context, messages []Message, system SystemPrompt,
           tools []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error)
}
```

### Tool（工具接口）
```go
type Tool interface {
    Info() Definition
    Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result
}

type Effectful interface {
    Effect() Effect  // "read" | "write" | "exec" | "mode"
}
```

### protocol.Action（UI → Runtime）
```go
type Action interface{ isAction() }
// InputAction, ConfirmAction, CancelAction, SwitchModelAction,
// ApprovePlanAction, RejectPlanAction, AnswerQuestionAction,
// QueueInputAction, CyclePermissionModeAction, LoadSessionAction,
// ListModelsAction, ClearHistoryAction
```

### protocol.Event（Runtime → UI）
```go
type Event interface{ isEvent() }
// SessionCreated, UserMessageAdded, AssistantDelta, ToolCallStarted,
// ToolExecDelta, ToolExecCompleted, StreamStarted/Completed,
// PlanApprovalRequested, QuestionAsked, TurnCompleted, ...
```

### Session Store（持久化接口）
```go
type Store interface {
    Create(ctx, title) (*Session, error)
    AppendMessage(ctx, sessionID, msg) error
    LoadMessages(ctx, sessionID) ([]json.RawMessage, error)
    List(ctx) ([]Session, error)
    Get(ctx, id) (*Session, error)
    Rename(ctx, id, title) error
    Delete(ctx, id) error
    UpdateMeta(ctx, sessionID, meta) error
}
```

## 关键设计

### EngineMediator 模式
- **A-class Action**（Input/Confirm/Cancel）：直接委托给 `Engine`
- **B-class Action**（SwitchModel/LoadSession/ListModels/CycleMode）：`EngineMediator` 处理，协调 Engine + Store + Provider

### 事件桥接
- `chat.Event`（内部）→ `chat.ToDTO()` → `protocol.Event`（DTO）→ `Engine.emitEvent()` → `eventCh` → UI 消费
- UI 只依赖 `protocol` 包，不依赖 `chat` 内部类型

### 流式输出处理
- `ModelStreamer.Stream()` 消费 SSE channel，逐事件拆解：
  - `text_delta` → `UIAssistantDelta`
  - `tool_use` 增量组装 → `UIToolCallDelta`
  - `thinking_delta` → `UIThinkingDelta`
  - `message_stop` → 组装完整 `modelResponse`

### 工具执行并行化
- `ToolExecutor.ExecuteBatch()` 为每个工具调用启动 goroutine
- 结果通过 channel 收集，保持顺序
- `chanEmitter` 将工具输出实时推送为 `UIToolExecDelta`

### 输入队列
- Agent 忙碌时用户输入进入 `userInputQueue`
- 每轮工具执行后 `DrainQueuedInputs()` 取出
- 队列输入作为 User 消息注入，触发 `UIQueuedInputPromoted`

### 输出截断自动升级
- 首次 `max_tokens` 限制（默认 16384）
- 若 `stopReason == "max_tokens"`，静默重试升级到 64000
- 发出 `UITruncationRetry` 事件供 UI 调试

### Prompt Caching
- Stable/Session 层通过 `CacheControl: {"type": "ephemeral"}` 标记
- Anthropic API 对相同前缀缓存，减少重复 token 计费
- `AssembleResultToSystemPrompt()` 将 `PromptSegment.Layer` 转为 `SystemBlock.CacheControl`

## 协议客户端

| 协议 | 包 | 认证方式 | 特殊字段 |
|------|-----|---------|---------|
| Anthropic | `claude` | x-api-key / Bearer | extended thinking |
| Aiden | `aiden` | API Key + AuthHelper | 消息格式转换 |
| Codebase | `codebase` | API Key + AuthHelper | config_name |

所有客户端均实现 `chat.ModelClient` 接口，通过 `createClient()` 工厂函数根据 `protocol` 字段选择。

## 实施阶段

### Phase 1: 项目脚手架 ✅
- Go 模块初始化、Bubble Tea v2 集成
- CLI 入口 + TUI REPL 骨架

### Phase 2: ModelClient 抽象层 ✅
- `ModelClient` 统一接口 + `ApiStreamEvent` 类型
- Anthropic provider（原生 tool_use + thinking）
- Aiden provider（消息格式转换）
- Codebase provider（config_name 支持）

### Phase 3: 工具系统 ✅
- `Tool` 统一接口 + `Effect` 分级
- 九个工具实现（Read/Write/Edit/Bash/Grep/Glob/AskUser/PlanMode/Skill）
- `Registry`：注册、查询、并行执行、输入校验

### Phase 4: Agent 循环 ✅
- `TurnRunner` ReAct 循环（并行工具调用）
- `ModelStreamer` 流式拆解
- `InteractionGate` 交互拦截
- `ToolExecutor` 并行执行
- 输出截断自动升级重试

### Phase 5: 权限系统 ✅
- 三级模式（Default/AutoAccept/Plan）
- Plan 模式：只读探索 + 计划文件写入 + 用户审批
- `InteractionGate` 自动放行逻辑

### Phase 6: Prompt 系统 ✅
- 三层架构（Stable/Session/Turn）
- Token 预算分配 + 两阶段估算
- Prompt Caching 支持
- CLAUDE.md 项目指令注入

### Phase 7: 会话持久化 ✅
- JSONL 文件存储（append-only）
- 会话创建、恢复、标题生成
- 元数据持久化（model、context window、token 统计）

### Phase 8: 技能系统 ✅
- SKILL.md 文件发现与解析
- 内置技能 + 项目级技能
- `/skill` 斜杠命令加载

### Phase 9: 配置系统 ✅
- `.cece/settings.json` 加载
- 环境变量兜底（ANTHROPIC_API_KEY/MODEL/BASE_URL）
- 多 provider 配置 + 模型上下文窗口映射

### Phase 10: 打磨与优化 🔄
- UI 体验优化
- 错误处理完善
- 边界情况处理
