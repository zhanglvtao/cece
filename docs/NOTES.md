# CC 开发过程中遇到的问题

## Plan Mode 空计划审批问题（2026-06-14 已修复）

### 问题现象
Agent 在 Plan Mode 中没有写出有效计划时调用 `ExitPlanMode`，UI 仍然弹出空计划审批，导致用户被要求批准一个不存在的 Plan。

### 根因
审批事件在工具执行前由 `InteractionGate` 发出。旧逻辑只要看到 `ExitPlanMode` 就发送 `PlanApprovalRequested`，没有先确认 `plan_file` 是否存在且内容非空。虽然 `ExitPlanMode` 工具执行阶段会拒绝空文件，但用户已经先看到了空审批弹窗。

### 修复
将“是否有可审批 Plan”的校验前移到 `InteractionGate`：只有读取到非空 plan 内容时才发审批事件；空文件、缺失文件或无效路径不弹审批，直接让 `ExitPlanMode` 执行并返回明确错误，保持 Plan Mode。

---

## Edit 工具 Tab 匹配问题（2026-06-03 已修复）

### 问题现象
Edit 工具调用时，如果文件使用 tab 缩进（如 Go 源码），LLM 构造的 `old_string` 经常无法匹配文件中的实际内容，导致编辑失败。

### 根因

**1. Read 工具输出混淆**

Read 工具输出格式为 `行号\t内容`，如果文件内容本身包含 tab（缩进），LLM 看到的输出中同时存在「展示用 tab」和「内容用 tab」，视觉上无法区分。LLM 构造 `old_string` 时可能多/少 tab。

修复：`read.go` 行号后分隔符从 `\t` 改为 `| `。

**2. Edit 工具双向标准化的架构缺陷**

旧实现 `findActualString` 对 file content 和 old_string **双向**做标准化（CRLF、tab/space、trailing ws、quotes），每次标准化都复制整个文件内容。这有两个问题：
- **性能差**：对大文件做 4 次 `strings.ReplaceAll` 复制
- **语义错误**：应该只变换 `old_string`（LLM 输出），适配原文件内容去搜索，而不是把文件内容也做变换

修复：重构为**单向变换**——只生成 `old_string` 的候选变体（tab↔spaces、CRLF↔LF、弯引号↔直引号），用 `strings.Index` 在原文件内容中直接搜索（零拷贝）。

### 参考
- opencode 的 `Replacer` 模式：只变换 old_string，逐行比较，零拷贝
- crush 的简单策略：只做 CRLF→LF，不做 fuzzy matching

---

## Edit 工具前导特殊字符匹配问题

### 问题现象
Edit 工具的 `old_string` 参数需要精确匹配文件内容，但 LLM 经常在 `old_string` 的前导空白（tab/space 混合）上出错。

### 根因
LLM 的 tokenization 对空白字符不敏感，特别是：
- Tab 和空格的混合缩进容易搞混
- 前导空格数量经常不对（多了或少了）
- 行尾空白容易被 LLM 忽略

### 缓解
Edit 工具的 fuzzy matching cascade 逐步尝试多种标准化策略，增加容错。但更好的方案是从源头减少歧义（如 Read 输出改用无歧义分隔符）。

---

## TUI 对话过长后卡死（2026-06-16 已修复）

### 问题现象
Cece TUI 在对话轮次增多、内容变长后会逐渐卡顿，最终完全卡死无法操作。

### 根因

**每事件全量 viewport 刷新**：`applyEvent()` 末尾无条件调用 `refreshViewport()`，而 `refreshViewport()` 做全量 `transcript.render()` + `viewport.SetContent()`。当 `globalEventMsg` 批量处理 8+ 个事件时，每个事件触发一次全量渲染——8+ 次 glamour markdown 渲染 + viewport 内容重建。

随着对话变长：
- blocks 数量 O(n) 增长
- 每个 dirty block 调用 `glamour.Render()`（最重操作）
- `rendererMu` 全局互斥锁串行化
- `viewport.SetContent()` 传入巨大字符串后 viewport 自身也要算行高

O(blocks) × O(events/frame) → TUI 冻结。

### 修复

将 viewport 刷新从 `applyEvent()` 延迟到 `View()` → `resize()` 中。`applyEvent()` 只设 `viewportDirty = true`，真正的刷新在每帧渲染时只做一次。`scrollToPlanBlock` 逻辑合并到 `refreshViewport()` 避免重复 `SetContent()`。

### 参考
- crush 使用 per-section 缓存 + 增量 glamour render（`streamingMarkdown` stable prefix 机制）
- bubbletea viewport 的 `SetContent` 是 O(content) 操作，越长的内容越慢

---

## Responses API reasoning item 丢失导致 400 错误（2026-06-17 已修复）

### 问题现象

使用 gpt-5.4（Responses API）时，第一轮模型返回了 reasoning + function_call，cece 执行工具后发送第二轮请求，API 返回 400：

```
Item 'fc_0e4ba...' of type 'function_call' was provided without its required 'reasoning' item: 'rs_0e4ba...'
```

### 根因

Responses API 是有状态的：如果上一轮响应包含了 reasoning item（`rs_...`）和 function_call item（`fc_...`），下一轮请求的 input 必须把它们都发回去。

cece 在 `stream.go` 中解析 SSE 事件时，`response.reasoning_text.delta` 和 `response.reasoning_summary_text.delta` 事件只在 `state.reasoningOpen == true` 时处理，而 `reasoningOpen` 只在 `response.output_item.added`（reasoning）事件中设置为 true。

如果 aiden proxy 不发送 `response.output_item.added`（reasoning）事件，或者该事件在 `reasoning_text.delta` 之后才到达，reasoning block 就不会被创建，导致下一轮请求缺少 reasoning item。

### 修复

1. **回退逻辑**：`stream.go` 中 `response.reasoning_text.delta` 和 `response.reasoning_summary_text.delta` 事件处理增加回退——当 `reasoningOpen == false` 时自动创建 reasoning block（发出 `content_block_start`），确保 reasoning 内容不丢失。

2. **诊断日志**：在 `stream.go` 的 `output_item.added`、`output_item.done`、`reasoning_text.done` 等关键事件处加 DEBUG 日志；在 `client.go` 的 `streamResponses` 中记录 reasoning/function_call item 数量和 ID 列表。

3. **`output_item.done` 改进**：当 `reasoningOpen == false` 时不再静默跳过，而是记录 reasoning item 的 ID 和 encrypted_content 供调试。

### 教训

- Responses API 的有状态特性要求 input 中必须包含之前响应的所有 output items
- SSE 事件顺序不能假设——proxy 实现可能改变事件顺序或省略某些事件
- 关键数据的丢失应该尽早检测和恢复，而不是等到 400 错误才发现
