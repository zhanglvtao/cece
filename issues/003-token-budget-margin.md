# Issue 003: Token Budget safety margin 过小

## 问题描述

当前上下文预算校验使用固定 `1024 tokens` 作为 safety margin：

```text
estimatedInput + maxTokens + 1024 <= contextWindow
```

这个值偏小，更像最小缓冲，不足以覆盖 token 估算误差、provider 计数差异、工具 schema 膨胀和协议额外开销。

相关代码：[turn_runner.go:284-331](file:///Users/bytedance/cece/internal/agent/turn_runner.go#L284-L331)

## 根本原因

Token 估算不是精确账本：

- 本地估算和 provider 真实计数可能不一致
- 不同 provider 对 system、tools、thinking、tool_result 的计数规则不同
- 工具 schema 数量增加会放大误差
- 长会话中 tool_use / tool_result 结构化块会带来额外协议开销

固定 `1024` 无法覆盖这些动态因素。

## 风险

- preflight 判断通过，但 provider 仍返回 context 超限
- auto compact 触发过晚，用户体验变成失败后补救
- 模型输出空间被压得过小，导致 `max_tokens` 截断
- 多 provider 场景下行为不稳定

## 修复建议

将固定小 margin 改为 **大 reserve + 动态校准**：

```text
reserve = max(
  20000,
  modelMaxOutput,
  contextWindow * ratio,
  toolSchemaTokens * factor,
  recentEstimateError
)

estimatedInput + reserve <= contextWindow
```

建议策略：

- 默认保留 20K 级别的大 reserve，不把窗口吃满
- 小窗口模型使用更高比例 reserve，并设置上限防止挤压输入
- 工具 schema 越大，reserve 越大
- 根据 provider 返回的真实 `input_tokens` 校准估算误差
- 对 context 超限错误记录 estimate vs actual，用于后续水位调整

## 优先级

**P1**：不影响普通短会话，但会影响长会话和多 provider 稳定性。
