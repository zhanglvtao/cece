# 开发问题记录

## tool_result 请求摘要与工具块生命周期分离
- 现象：工具执行后，下一次模型请求会单独显示 `[tool_result] estimated input...`，和刚刚完成的工具块割裂，尤其 `Grep` 这类高频工具会制造很多噪音。
- 定位：`ModelRequestStarted{Reason:"tool_result"}` 和 `ToolExecCompleted` 是两条独立事件；UI transcript 之前直接把前者渲染成 `blockInfo`，没有尝试关联最近工具块。
- 结论：这类运行时元信息应挂在对应 `blockTool` 上；只有找不到工具块时才降级为独立 info，避免丢诊断信息。

## 默认配置没有贯穿 runtime/UI 导致状态栏漂移
- 现象：默认推理强度期望为 `xhigh`，但进入 TUI 会话状态栏显示 `high`，首轮普通输入后也被 `EffortChangedEvent(high)` 覆盖。
- 定位：config 已读取 `provider.effort`，但 `cmd -> runtime.Options -> BuildRequest -> Engine` 链路没有传递；TUI `NewModel` 还写死了 `high` 初始值。
- 结论：默认值必须只在 config 层落地一次，再通过 runtime/EngineReadyEvent 同步给 engine 与 UI；UI 不应该自行猜默认值。

## Codebase provider token 与 coco 插件模型
- 现象：用 cece 现有 Aiden auth helper 返回的 1460 长度 token 调 TraeV2 `/chat/completions`，18 个 coco `byted_trae` 模型全部 401。
- 定位：coco 插件里的 `${CODE_USER_JWT}` 对应 Codebase JWT，不是 Aiden/ByteCloud token。
- 验证：`bytedcli auth get-codebase-jwt-token` 返回的 299 长度 token 可用；18 个模型最小 headers 均返回 `event: output`/`done`。
- 结论：codebase provider 默认 auth helper 应该是 `bytedcli auth get-codebase-jwt-token`；headers 最小集 `Authorization` + `X-Coco-Business-ID` 已够用。

## Coco 插件 YAML 里存在 macOS AppleDouble 文件
- 现象：扫描 `~/Library/Caches/coco/plugins/*/*.yaml` 时会遇到 `._traecli.yaml`，内容不是 UTF-8 YAML，解析报 `special characters are not allowed`。
- 结论：插件模型发现必须只读取明确文件名 `coco.yaml` / `traecli.yaml`，不要 glob 全部 `*.yaml`。
