# Issue 004: tool result 缺少结构化引用导致 Trim 后丢失可追溯性

## 问题描述

大工具输出会落盘到 `.cece/tool-results`，对话里只返回预览和文件路径。但当前文件路径主要存在于 `tool_result.Content` 文本中。

历史 Trim 会把 `tool_result.Content` 整体替换成 `[trimmed]`，如果引用只存在于文本里，Trim 后完整结果路径会丢失。

相关代码：
- 大结果落盘：[result_storage.go:22-60](file:///Users/bytedance/cece/internal/tool/result_storage.go#L22-L60)
- Trim 替换内容：[message.go:431-439](file:///Users/bytedance/cece/internal/agent/message.go#L431-L439)
- 全量截断：[truncate.go:6-21](file:///Users/bytedance/cece/internal/agent/truncate.go#L6-L21)

## 根本原因

`tool.Result` 已经有结构化字段：

```go
OutputPath
OriginalBytes
PreviewBytes
```

但回填给会话历史的 `ApiToolResultBlock` 只保留了：

```go
ToolUseID
Content
IsError
Truncated
TotalLines
```

结果引用被降级成普通文本，无法在 Trim / Compact / Prune 中可靠保留。

## 风险

- Trim 后无法再通过路径读取完整工具输出
- 大输出的可追溯性被破坏
- Compact 只能基于残留文本总结，无法引用原始证据
- Multi-Agent 或长任务复盘时缺少结果 artifact

## 修复建议

tool result 必须结构化。将结果 artifact 元数据提升到 `ToolResultBlock`：

```go
type ApiToolResultBlock struct {
    ToolUseID      string
    Content        string
    IsError        bool
    Truncated      bool
    TotalLines     int
    OutputPath     string
    OriginalBytes  int
    PreviewBytes   int
}
```

Trim 规则调整为：

```text
可以裁掉 Content 预览
必须保留 OutputPath / OriginalBytes / PreviewBytes
```

推荐显示语义：

```text
[trimmed preview]
Full output saved to: .cece/tool-results/xxx.txt
```

## 优先级

**P1**：影响长会话上下文压缩后的可追溯性，是上下文管理的结构性缺口。
