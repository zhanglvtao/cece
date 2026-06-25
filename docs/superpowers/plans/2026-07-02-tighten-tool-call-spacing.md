# TUI 工具调用块紧凑化方案

## Context
Cece 的 TUI 渲染中，transcript 里的每一个 block（工具调用、assistant 消息、thinking 等）之间一律使用 `\n\n` 分隔，导致连续的工具调用链（例如 Grep → Read → Bash）之间视觉间距过大，信息密度低，一眼扫不到执行流程。需要在保持跨语义块（tool ↔ assistant ↔ user）层次分明的前提下，把工具链内部紧凑化。

## Approach
修改 `transcript.render()` 中 block 之间的分隔逻辑，根据**相邻两个 block 的类型**决定分隔符：

1. 抽出一个辅助函数 `blockGap(prev, next blockKind) string`：
   - 若 `prev` 和 `next` **均为 `blockTool`** → 返回 `"\n"`（1 行间隔，紧凑）
   - 其他所有组合 → 返回 `"\n\n"`（2 行间隔，保留语义层次）
2. 在 `render()` 的循环里，将原先的 `b.WriteString("\n\n")` 替换为调用 `blockGap()`，传入上一个 block 和当前 block 的 `kind`。
3. 新增 unit test：
   - 构造连续 3 个 tool block（Grep/Read/Bash），断言两两之间无 `"\n\n"` 块间空行。
   - 构造 tool → assistant → tool 序列，断言 tool 与 assistant 之间仍是 `"\n\n"`。
   - 使用 `stripAnsi()` + 带锚定标签的断言（如 `"Grep ✓\nRead"` / `"Grep ✓\n\nCece"`），避免 block 内部换行干扰。

## Files to modify
- `internal/ui/transcript.go`
  - 新增 `blockGap(prev, next blockKind) string`。
  - `render()` 内循环：记录前一个 block 的 `kind`，用 `blockGap` 替换硬编码的 `"\n\n"`。
- `internal/ui/model_test.go`（沿用现有 test 模式，不新建文件）
  - `TestToolBlocksSingleGapBetweenTools`
  - `TestGapBetweenToolAndAssistantIsDouble`

## Reuse
- 复用 test 构造模式：`NewModel(nil, "sonnet", "/tmp")` + `ApplyEventForTest` 注入事件。
- 复用 `stripAnsi()` 辅助函数清理颜色码后做字符串断言。
- `transcriptBlock.kind`、`render()`、`renderOrderIndices()` 均为现有结构。

## Verification
```bash
cd /Users/bytedance/cece
go test ./internal/ui/... -run 'TestToolBlocksSingleGapBetweenTools|TestGapBetweenToolAndAssistantIsDouble' -count=1
go test ./internal/ui/... -count=1   # 不破坏现有 UI 用例
```
手动验收：启动 cece，触发连续 Grep/Read/Bash，确认工具之间视觉紧凑，tool 与 assistant 仍有分段。
