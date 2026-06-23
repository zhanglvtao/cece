# 开发问题记录

## Compact 失败后必须有兜底上下文管理
- 现象：长会话里模型调用 `Compact{"turn":151}`，当前总 turn 数也是 151；工具按最大合法 turn=150 判错，随后剩余上下文低于 20% 也没有自动降级压缩。
- 定位：`Compact` 的 turn 语义本应是“保留从该 turn 开始”，因此 `turn == totalTurns` 应代表末尾边界；同时 `TryAutoCompact` 只在 assistant 回复后按 used>=90% 尝试一次 Compact，工具失败写入 `tool_result` 后没有进入兜底链路。
- 结论：turn 边界应按半开区间处理并 clamp 到末尾；剩余上下文 <20% 时必须执行 `Compact -> TrimToolResults -> Prune` 预算保证器，且触发点要放在 tool_result 写入后、下一次模型请求前。

## HistoryClearedEvent 的累计 token 与当前 ctx 水位分离
- 现象：TUI 收到 `HistoryClearedEvent` 后 transcript 已清空，但底部 ctx 状态栏仍显示清理前的剩余上下文，例如 `20K/200K 10%`。
- 定位：`transcript.reset()` 同时保留累计 token 统计和 `contextUsed`；前者是会话累计指标，后者是当前 API 请求上下文水位，`/clear` 后应归零。
- 结论：reset 可以保留 input/output/cache 累计统计，但不能保留 `contextUsed`；`SessionLoadedEvent` 这类恢复场景应在 reset 后显式写回 `LastInput`。

## Plan rejection 必须补齐 tool_result
- 现象：长会话执行 Compact 时 Aiden 返回 `No tool output found for function call ...`，TUI 又把失败误显示成 `Not enough messages to compact`。
- 定位：`ExitPlanMode` 被用户拒绝/打断时，assistant 的 `tool_use` 已进入 history，但 `TurnRunner` 直接 `PlanRejected` 后结束，没有追加对应 user `tool_result`；下一次普通 request 或 compact summary 都会携带 orphan tool_use。
- 结论：所有进入 history 的 tool_use 都必须在同一历史流里补齐 tool_result；plan rejection 也应复用 `rejectToolResults`，但不立即 continue，等下一次用户反馈自然带上拒绝结果。

## Agent Observatory 控制面与观测面边界
- 现象：设计 Web Observatory 时，最初尝试让 TUI 通过 IPC action 把快照发回 runtime-side hub，甚至让 Hub 成为 Engine event 的 fan-out 出口；这会让观测系统夹进 TUI → Engine 控制链路，语义别扭且有扩大耦合风险。
- 定位：TUI 的 `Input/Action` 是业务控制面，必须直接到 `RuntimeHost/Mediator/Engine`；Observatory 只能是旁路观察者。Engine event channel 是 single-consumer，又需要同时给 TUI 和 Web，所以正确边界是 `EventTapRuntime`：唯一读取 `Base.Events()`，一份转发给 IPC/TUI，一份送 sidecar Hub。
- 结论：Observability Hub 不拥有控制面，也不应该伪装成 action sink。TUI 本地状态通过 HTTP `/api/snapshot` 上报到 Hub；Engine events 通过 event tap 被复制到 Hub，避免多 reader 抢 channel 丢事件。

## StatusBar ctx 文档与实现语义容易漂移
- 现象：README 里仍示例 `ctx:150K/200K 75%` 并描述为 usage percentage，但当前 TUI 实现实际渲染 10 格剩余上下文 gauge，百分比也是剩余比例。
- 定位：ctx 文案和颜色逻辑集中在 `internal/ui/statusbar.go`，但对外文档没有跟随底部 cockpit 改版更新。
- 结论：状态栏 UI 一旦调整文案/语义，应同步更新 README；尤其“used vs remaining”这种语义会直接影响阈值告警判断。

## assistant markdown 外层套色与内部高亮的边界
- 现象：给 cece 正文整体加主色时，直接在渲染后的 markdown 外层套 `lipgloss.Style` 最简单，但可能覆盖 markdown 内部已有的 heading/link/code 颜色。
- 定位：assistant 流式/完成态分别在 `renderStreamingAssistant` 和 completed assistant 分支拼接 markdown 输出；markdown 颜色来源是 `buildGlamourStyle`。
- 结论：短期为满足正文整体主色与缩进，用独立 `AssistantBody` 样式集中处理；若后续发现 markdown 内部高亮被压平，应把默认正文色下沉到 glamour `Document`/`Paragraph`，不要在所有输出外层强套。

## transcript 标签样式分散导致一致性风险
- 现象：transcript 主渲染、streaming assistant/thinking、Todo List 分别手写 `[` `]` 和 label 文案，改视觉样式时容易漏分支。
- 定位：`renderBlock`、`renderStreamingAssistant`、`renderStreamingThinking`、`renderTaskBar` 各自拼接 label。
- 结论：transcript label 应集中格式化，面板标题也要有明确常量/上限，避免局部 UI 风格漂移。

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

## Agent 异步完成通知不能只走 UI event
- 现象：worker agent 已经通过 `SubAgentCompletedEvent` 通知 TUI 完成，但 foreground LLM 不会自动知道；UI event 不进入模型上下文，前台 agent 只能自己记得轮询 `Agent(status)`。
- 定位：Agent 间异步通信需要模型可见通道。event stream 是观察层，不能承担 agent IPC；artifact path 写入后也必须回填 runtime result，否则后续 status/wait 拿不到完整交付物。
- 结论：worker terminal/pending 要写 parent inbox，在 foreground 下一次 model request 前作为 synthetic notification 注入；`Agent(wait)` 是不可见等待，不通知 worker。

## 真实 LLM 录制测试需要显式开关
- 现象：`internal/evals/recording` 已有 cassette record/replay 框架，但缺少一条真实 provider 的录制入口；如果把真实 LLM 调用混进普通单测，会造成网络、鉴权和费用不稳定。
- 定位：真实录制应复用 `RecordingClient` 包装 provider client，并通过环境变量显式启用；默认 `go test ./...` 只能验证 replay/序列化框架，不应发真实请求。
- 验证：用 aiden `glm-5.1` 实跑 `CECE_RECORD_LLM=1 ... TestRealLLMRecord_AidenGLM51`，生成 `internal/evals/testdata/aiden-glm-5.1-basic.cassette.json` 并立即 replay 通过。
- 结论：新增 aiden `glm-5.1` 的 env-gated record 测试，`CECE_RECORD_LLM=1` 时才录制 cassette，并立即 replay 验证 cassette 可用。

## SWE-bench patch 采集不能混入 harness 注入文件
- 现象：SWE-bench runner 在容器里注入 `SYSTEM.md` 和 `issue.md` 后，`get_patch()` 执行 `git add -A` 只 reset 了 `.cece/`，导致输出预测 patch 包含 prompt artifact，不是纯源码修复。
- 定位：`swebench/docker.py` 的 patch 边界应该只包含 agent 对仓库源码的修改；评测 harness 写入的控制文件必须在 cached diff 前排除。
- 结论：生成 patch 时同时 reset `.cece/`、`SYSTEM.md`、`issue.md`；后续新增任何 harness 注入文件，都必须加入同一排除边界，否则会污染 SWE-bench prediction。

## SWE-bench auto-accept 会绕过 plan reminder
- 现象：`astropy__astropy-7746` 中 gpt-5.5-paygo 只修了 `np.zeros((0, 2))` 空输入，没有覆盖 issue/test 里的 `([], [1])` 调用形态，官方判分 2/3 resolved。
- 定位：SWE-bench harness 为了无人值守把 `defaultMode` 强制成 `auto-accept`，导致 agent 首轮缺少 plan mode reminder；而 yolo 已可自动放行 `ExitPlanMode`，不需要牺牲 plan-first 流程。
- 结论：评测容器应使用 `defaultMode=plan` + `yolo.enabled=true`：让模型先规划复现和边界，同时避免计划审批卡住 batch runner。

## Prompt 分层不能只补 plan reminder
- 现象：`astropy__astropy-7746` 改为 plan mode 后仍只修了一半，说明 runtime planning shell 本身不足以保证模型覆盖所有失败输入形态。
- 定位：完整 prompt 行为需要 Stable 的 completion/verification/failure-diagnosis contract、Turn 的 task-aware reminder、Plan reminder 的收敛标准协同；只强化任一层都会留下 half-fix 风险。
- 结论：bugfix 类任务的通用约束应放 Stable；每轮按输入触发的复现提醒应放 Turn；plan mode 只负责规划协议和审批边界，不承担全部实现质量保证。

## 默认 plan mode 需要显式告诉模型当前状态
- 现象：会话启动默认就是 plan mode，但模型仍可能先调用 `EnterPlanMode`，随后工具返回 `Already in plan mode.`。
- 定位：tool definitions 里仍包含 `EnterPlanMode` 是为了保持工具结构稳定；模型是否知道“已在 plan 中”取决于模型可见的 plan reminder。
- 结论：不要为了避免重复调用而动态移除工具，优先增强 full plan reminder 的当前状态表述，例如 `You are already in plan mode.`。

## Kaboo 本地 ledger 的分类字段不能等同于原生日志
- 现象：cece 接入 Kaboo 使用量上报时，最稳的边界不是直连 Kaboo API，而是写 botmux 已支持的 `~/.botmux/usage/usage-YYYY-MM-DD.jsonl`。
- 定位：Kaboo `report` 会扫描本地 usage ledger 后统一聚合上报；ledger 的 `cliId` 可按产品分类，但不代表本地存在对应 CLI 的原生 transcript。
- 结论：按需求把 cece ledger 的 `cliId` 固定为 `claude-code`，但只写 botmux-compatible ledger，不伪造 Claude Code 原生日志，避免后续 native parser 与 ledger 双计。

## 发布前敏感信息扫描默认边界
- 现象：发布前如果让大模型临时扫敏感信息，慢、不可复用，也容易把 `.cece`、`.claude`、构建产物等本地文件混进判断。
- 定位：发布风险来自会被推送的 Git 内容；脚本默认应以 `git ls-files` 为输入，只检查版本控制内文本文件。
- 结论：敏感信息扫描脚本默认只扫 Git 跟踪文件，命中时输出 `file:line: rule` 并失败；本地缓存和未跟踪私有配置不属于发布前默认扫描边界。

## Write 工具 UI diff 预览的跨层契约
- 现象：UI 已把 `Write` 当作 diff tool 截断/高亮，但工具本身只返回 `wrote N bytes`，导致 report 只能看到参数，真正的文件变化不可见。
- 定位：`internal/tool/write.go` 是结果语义来源；`internal/ui/transcript.go` 只负责渲染与预览策略。只在 UI 隐藏参数会丢信息，必须让 Write 返回和 Edit 一致的 unified diff。
- 结论：工具结果格式和 UI 展示策略要成对演进；diff 的“10 行内”还要把隐藏/截断提示行计入预算，避免视觉上超过约定。

## Plan mode 权限与 visual companion artifact 冲突
- 现象：brainstorming 的 visual companion 需要把 HTML mockup 写到 `.superpowers/brainstorm/.../content`，但 plan mode 只允许写 `.cece/plans`，导致只能把完整文本 mockup 贴回对话。
- 定位：权限在 `InteractionGate` 和 `ToolExecutor` 双层判断，提示词也写死“只能编辑 plan file”；安全边界把所有写入都按 code edit 处理，漏掉了计划阶段的非代码 artifact。
- 结论：plan mode 写权限应按 artifact 路径白名单建模，默认允许 plan 文件和 mockup content，额外范围走配置注入，不能放开整个项目写入。

## Web topology 手写坐标会让视觉连接与语义边漂移
- 现象：Agent Observatory 页面里的线条是固定 ASCII 文本坐标，工具节点又共用同一位置，运行中多个 tool edge 很容易和实际模块对不上或重叠。
- 定位：`Store` 已输出 `ObservatoryNode/ObservatoryEdge` 语义关系，但 Web 层用 `slots/edgeSlots` 二次硬编码坐标，没有把边连接到真实节点端点。
- 结论：观测层应保持语义边由 Store 产生，端点连接、自动布局和多边避让交给图渲染框架；否则拓扑一扩展视觉就会漂移。

## Observatory 前端 embed 的构建顺序
- 现象：React Flow Observatory 运行时已经通过 Go `embed.FS` 随二进制分发，但 `build.sh` / source fallback 只跑 `go build` 时，binary 会直接携带仓库里已有的 `webapp/dist`，前端源码变化可能没有进入产物。
- 定位：`go:embed` 只在 Go 编译时读取磁盘上的 dist 文件，不会触发 Vite 构建；构建链必须显式先跑 `npm ci` / `npm run build`，再跑 `go build`。
- 结论：前端 Observatory 是运行时零 Node 依赖，但构建时有 Node/npm 依赖；所有发布/安装/交叉编译入口都要先刷新 dist，再编译 Go binary。

## Observatory 高频事件会造成拓扑闪烁与 Evidence 失真
- 现象：React Flow 拓扑在流式事件期间频繁闪烁，甚至看起来短暂消失；Evidence 面板大量显示 `ToolCallDelta` / `StreamEventDetail` 这类类型名，缺少可读细节。
- 定位：前端每次 SSE state 更新都重新跑自动布局并触发 `fitView`，高频 delta 事件会不断重置视口；后端 `eventSummary` 对大多数事件没有摘要，Evidence 又只显示最近 12 条短文本。
- 结论：拓扑布局要与高频 Evidence 更新解耦，视口只在首次加载时 fit；Evidence 应是可读事件日志，过滤无意义 delta，并保留 kind、summary、detail。

## Observatory 主链路和观测旁路不能混画
- 现象：拓扑把 `runtime → hub → engine` 画在主链路中，容易误解为 Observatory Hub 负责调度 Engine，也导致 TUI Client 和 Engine 之间看不到真正的控制路径。
- 定位：Hub 实际是观测旁路，负责收集事件、写入 Store、通过 SSE 推给 Web；业务控制路径应是 `user → tui → runtime → engine → model`。
- 结论：拓扑语义要区分 control path 和 telemetry path；观测 Hub 应作为旁路节点，只接收 telemetry，不指向业务模块。

## @ 文件弹窗被深层大目录饿死
- 现象：在大仓库根目录输入 `@dbatman` 时，`dbatman/` 真实存在但弹窗为空。
- 定位：`FileWalker` 用深度优先 `filepath.Walk` 扫描，并有 5000 条全局上限；字典序靠前的巨大子目录会先耗尽配额，根目录后续目录无法进入候选缓存。
- 结论：面向交互补全的索引应浅层优先，先保证根目录和近层目录可见，再用上限控制成本；不要简单调高上限。
