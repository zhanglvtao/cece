# cece 架构规划

## 项目定位

生产级 code agent CLI，多供应商 LLM 后端，从零构建中学习 agent 架构全部细节。

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 语言 | TypeScript | CLI agent 生态最佳，Claude Code 同栈 |
| LLM 后端 | Claude + OpenAI + Ollama | 不锁供应商，Ollama 降门槛 |
| 交互模式 | 纯 CLI REPL | 核心形态，IDE/Web 后续迭代 |
| 工具体系 | 最小集 + 可扩展架构 | Read/Write/Edit/Bash/Grep/Glob，MCP 预留 |
| Agent 循环 | 并行 ReAct | 支持单轮多工具调用，效率必需 |
| 流式输出 | Token 级流式 | 生产级硬性要求 |
| 权限模型 | 分级确认 | 读放行，写/执行需确认，安全标配 |
| 上下文管理 | 滑动窗口 + 摘要压缩 | MVP 后迭代 |
| 项目结构 | 单体包，按接口边界组织 | 严格接口，未来可拆包 |
| 终端 UI | ink (React for CLI) | 声明式渲染，流式友好 |
| 配置管理 | 本地文件优先 + 可选 git sync | 日常零网络，同步手动触发 |
| 对话持久化 | JSON 文件存储 | 透明、可编辑、够用 |
| 工具调用 | 混合（原生 tool_use 优先，prompt 降级） | 兼容不支持 function calling 的模型 |
| 测试 | 单元测试 + 集成测试 (Vitest) | 核心 logic 必须自动化验证 |
| 文件编辑 | Diff/patch (old→new) | 省-token、精准、跨语言 |
| Shell 执行 | spawn + 流式输出 + 超时 | 日常命令够用，交互式命令不属于 agent |
| System prompt | 模板引擎（变量注入） | 工具列表、项目上下文等动态注入 |
| 错误处理 | 指数退避重试 + LLM 自修正 | API 限流自动重试，工具失败让 LLM 调整 |
| 构建工具 | pnpm + tsup | 快速构建，单文件打包 |
| 分发 | npm 全局安装 | 开发者用户，Node.js 合理假设 |
| 日志 | 结构化日志 (pino) | --verbose 控制，JSON 格式可分析 |

## 项目结构

```
cece/
├── bin/
│   └── cece.mjs           # CLI 入口（shebang + 动态 import）
├── src/
│   ├── index.ts              # 应用入口，Commander 解析参数
│   ├── cli/
│   │   ├── app.tsx           # ink 主应用组件（REPL）
│   │   ├── components/       # UI 组件
│   │   └── commands/         # CLI 子命令（config sync 等）
│   ├── agent/
│   │   ├── loop.ts           # 核心 ReAct 循环
│   │   ├── message.ts        # 消息类型定义
│   │   └── context.ts        # 上下文管理
│   ├── providers/
│   │   ├── interface.ts      # LLM Provider 统一接口
│   │   ├── anthropic.ts      # Claude 实现
│   │   ├── openai.ts         # OpenAI 实现
│   │   ├── ollama.ts         # Ollama 实现
│   │   └── registry.ts       # Provider 注册与选择
│   ├── tools/
│   │   ├── interface.ts      # Tool 统一接口
│   │   ├── registry.ts       # 工具注册中心
│   │   ├── read.ts           # 文件读取
│   │   ├── write.ts          # 文件写入
│   │   ├── edit.ts           # Diff/patch 编辑
│   │   ├── bash.ts           # Shell 执行
│   │   ├── grep.ts           # 内容搜索
│   │   └── glob.ts           # 文件模式匹配
│   ├── permissions/
│   │   ├── checker.ts        # 权限检查
│   │   ├── rules.ts          # 权限规则
│   │   └── store.ts          # 白名单持久化
│   ├── config/
│   │   ├── schema.ts         # 配置结构定义
│   │   ├── loader.ts         # 配置加载（全局 + 项目级）
│   │   └── sync.ts           # Git 同步
│   ├── session/
│   │   ├── store.ts          # 会话 JSON 存储
│   │   └── types.ts          # 会话类型
│   ├── prompts/
│   │   ├── system.md         # System prompt 模板
│   │   └── renderer.ts       # 模板渲染
│   └── utils/
│       ├── logger.ts         # pino 封装
│       ├── token.ts          # token 计数
│       └── retry.ts          # 重试逻辑
├── tests/
│   ├── unit/
│   └── integration/
├── package.json
├── tsconfig.json
├── tsup.config.ts
└── vitest.config.ts
```

## 实施阶段

### Phase 1: 项目脚手架 ✅
- pnpm 初始化、tsconfig、tsup、vitest 配置
- 核心依赖安装
- CLI 入口 + ink REPL 骨架
- bin/cece.mjs 入口脚本（解决 ESM + shebang 兼容）

### Phase 2: Provider 抽象层
- LLMProvider 统一接口 + StreamEvent 类型
- Anthropic provider（原生 tool_use）
- OpenAI provider（function_calling）
- Ollama provider（prompt 降级）
- Provider 注册中心

### Phase 3: 工具系统
- Tool 统一接口 + ToolResult
- 六个核心工具实现
- 工具注册中心（生成 tool definitions）

### Phase 4: Agent 循环（核心）
- 消息类型系统
- System prompt 模板 + 渲染引擎
- ReAct 循环（并行工具调用）
- 错误处理与重试

### Phase 5: 权限系统
- 分级权限检查（read 放行，write/execute 需确认）
- 权限规则定义 + 白名单持久化
- PermissionPrompt UI 组件

### Phase 6: 配置系统
- 配置结构定义（全局 + 项目级）
- 配置加载与合并
- Git sync（push/pull）

### Phase 7: 会话持久化
- JSON 文件存储
- 历史会话列表与恢复

### Phase 8: 日志系统
- pino 结构化日志
- 级别控制（--verbose）

### Phase 9: 集成测试与打磨
- 真实 API 集成测试
- 边界情况处理
- 用户体验打磨

## 核心接口

### LLMProvider
```typescript
interface LLMProvider {
  name: string
  supportsToolUse: boolean
  chat(messages: Message[], options: ChatOptions): AsyncIterable<StreamEvent>
}
```

### Tool
```typescript
interface Tool {
  name: string
  description: string
  parameters: Record<string, unknown>
  permission: 'read' | 'write' | 'execute'
  execute(params: Record<string, unknown>, context: ToolContext): Promise<ToolResult>
}
```

### StreamEvent
```typescript
type StreamEvent =
  | { type: 'text_delta'; text: string }
  | { type: 'tool_use_start'; id: string; name: string }
  | { type: 'tool_use_delta'; id: string; input: string }
  | { type: 'tool_use_end'; id: string; name: string; input: Record<string, unknown> }
  | { type: 'error'; error: Error }
  | { type: 'done'; stopReason?: string }
```

## 验证标准

cece 能完成以下场景即达到 MVP：
1. 启动 → 输入自然语言 → agent 读取文件并回答
2. 要求修改文件 → agent 用 Edit 工具精准修改
3. 执行命令时弹出权限确认
4. 重启后恢复上次对话
5. 切换 provider 后功能正常
