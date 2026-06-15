# CC 开发过程中遇到的问题

## 1. 空响应导致 Agent Loop 提前停止

**日期**: 2026-06-15
**会话**: `52a69fe4` (dbatman 项目)
**现象**: Agent 在执行计划时突然停止，模型还有工作要做但 loop 退出了。

**根因**: `turn_runner.go` 中，当 LLM 返回完全空响应（无文本、无工具调用、无 thinking）时，代码写入了 `[Empty response — retrying]` 消息但**没有 `continue` 继续循环**，而是继续执行到 "no tool calls → finish turn" 分支，直接 `return` 退出了 agent loop。

**修复**: 空响应时 `continue` 继续循环重试，添加连续空响应计数器（最多 3 次）防止无限循环。

**教训**: LLM 偶尔会返回空响应（尤其是 gpt-5.4 via Aiden Responses API），agent loop 必须能正确处理这种情况——要么重试，要么优雅降级，但不能静默停止。

---

## 2. Edit 工具的前导特殊字符匹配问题

**现象**: Edit 工具在匹配 `old_string` 时，经常因为前导的特殊字符（如制表符 `\t`、空格数量不一致）匹配不上而失败。

**常见场景**:
- 代码中有 tab 缩进，但 LLM 生成的 `old_string` 用的是空格
- 行首有不可见的 Unicode 字符
- 空行数量不一致

**缓解**: Edit 工具实现中做了模糊匹配（忽略行首空白差异），但仍不能覆盖所有情况。

---

## 3. 事件流式（Event Streaming）处理相关问题

**现象**: SSE 流中事件可能丢失或乱序，导致 UI 状态不一致。

**常见场景**:
- 网络断开重连后，中间的事件丢失
- `response.completed` 事件缺失，导致 UI 一直显示 loading 状态
- Aiden Responses API 的 `incomplete_details` 字段指示响应被截断，但前端没有正确处理
