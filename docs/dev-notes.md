# CC 开发过程中的问题与踩坑

## 1. tool_result 消息使用了 UserRole 导致 split 边界错误

**日期**: 2026-06-24

**现象**: 调用 `TrimToolResults` 工具后，Aiden API 返回 400：
```
No tool call found for function call output with call_id call_Xoisr8RoYqFcLJTHmtlST1ZW
```

**根因**: 内部数据模型中 `Role` 只定义了 `UserRole` 和 `AssistantRole` 两种。tool_result 消息也用了 `UserRole`，导致 `Role` 字段承载了两种语义。凡是依赖 `m.Role == UserRole` 做 turn 边界判断的地方都有 bug：

- `splitMessagesForCompact` 把 tool_result 消息当成 turn 边界 → split 后 tool_use 和 tool_result 被分到不同组
- `TurnBoundaries` 同理，统计的 turn 数不准
- compact boundary 插入后，`MessagesAfterCompactBoundary` 跳过含 tool_use 的 summarize 组
- API 请求中出现孤立的 tool_result，Aiden proxy 报错

**修复**: 引入 `ToolRole = "tool"`，让内部模型对齐真实 API 语义。所有创建 tool_result 消息的地方改用 `ToolRole`，序列化层适配（aiden/codebase 输出 `role:"tool"`，claude 输出 `role:"user"`），`loadSession` 加旧数据迁移。

**教训**: 内部数据模型应该对齐外部 API 语义，不应该用同一个字段承载两种语义。当你发现需要 `IsToolResultMessage()` 这样的辅助函数来区分同一 Role 下的两种消息时，说明 Role 本身需要拆分。

## 2. Edit 工具的前导特殊字符匹配问题

**现象**: Edit 工具在替换 `old_string` 时，经常因为前导空格/制表符数量匹配不上而出错。LLM 生成的 `old_string` 与文件实际内容在空白字符上有微妙差异，导致精确匹配失败。

**教训**: 这是一个 LLM 工具使用中的经典问题——LLM 对空白字符的感知不够精确。可能的改进方向：模糊匹配（忽略前后空白差异）、或者让工具支持正则匹配。
