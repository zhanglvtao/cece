# Issue 001: IPC 通信层可靠性问题

## 概述

当前 TUI ↔ Engine 之间的 JSONL over stdio IPC 实现存在多个可靠性问题，可能导致消息丢失、通道断裂、UI 卡死、Engine 被拖死等故障。

本文档汇总三个相关子问题：
1. 单条消息 8MB 上限导致通道断裂
2. TUI 消费慢导致背压反传拖死 Engine
3. TUI 写 Action 阻塞可能卡死 UI

---

## 子问题 1：单条消息超过 8MB 导致 IPC 断裂

### 问题描述

当单条 JSONL 消息（一行 JSON）超过 8MB 时，`bufio.Scanner` 返回 `bufio.ErrTooLong`，导致 IPC 通道断裂。

主要风险在 **Engine → TUI** 方向：工具返回大结果（如大文件内容）时，`ToolExecCompleted` 的 result 字段可能超过 8MB。

TUI → Engine 方向几乎不会触发（Action 都是用户输入和控制指令）。

### 根本原因

JSONL 将"一行"与"一条消息"绑定，而 `bufio.Scanner` 必须把一整行读进内存才能解析。scanner max token size 设为 8MB，超过就报错。

相关代码：
- Engine 端：[stdio_engine.go:67-68](file:///Users/bytedance/cece/internal/ipc/stdio_engine.go#L67-L68)
- TUI 端：[client.go:101-102](file:///Users/bytedance/cece/internal/remote/client.go#L101-L102)

---

## 子问题 2：TUI 消费慢导致背压反传拖死 Engine

### 问题描述

TUI 渲染速度跟不上 Engine 产事件速度时，背压沿 IPC 通道一路反传，最终 Engine 核心 Agent 循环阻塞。

这违反了"进程分离"的初衷——拆进程本来是为了故障隔离，结果 UI 慢还是能把核心拖死。

### 根本原因

整条链路四级缓冲全部是阻塞写，背压完整传导：

```
TUI 渲染慢
  → Client.events channel 满 (4096)
  → readLoop emit 阻塞，不读 stdout
  → OS pipe buffer 满 (~64KB，内核管，应用层不可配置)
  → Engine 写 stdout 阻塞
  → Engine.eventCh 满 (4096)
  → emitEvent 阻塞
  → Engine 核心循环卡住
```

关键代码：
- Engine 阻塞写：[engine.go:668-670](file:///Users/bytedance/cece/internal/engine/engine.go#L668-L670)
- TUI 阻塞写：[client.go:125-130](file:///Users/bytedance/cece/internal/remote/client.go#L125-L130)

---

## 子问题 3：TUI 写 Action 阻塞可能卡死 UI

### 问题描述

TUI 调用 `client.write()` 发送 Action 时，如果 Engine 不读 stdin（如 Engine 卡死），`stdin.Write()` 会阻塞。若调用发生在 Bubble Tea Update goroutine，整个 UI 冻住。

### 根本原因

`client.write()` 是同步阻塞写，没有异步队列保护：

```go
// client.go:94
_, err = c.stdin.Write(append(line, '\n')) // 阻塞
```

Engine 不消费时，pipe buffer 满了，write 系统调用就阻塞。

---

## 统一修复建议

按工程性价比排序：

### 方案 A：分层解耦 + 异步缓冲（推荐，核心改法）

**核心思路**：在两端都加异步缓冲层，把"应用层逻辑"和"IO 传输"解耦。

#### Engine 侧改造
```
emitEvent → 环形缓冲（状态事件保证不丢，delta 可覆盖）→ 后台 goroutine → stdout
```
- 增量事件（Delta）用环形缓冲，满了覆盖最旧的
- 状态事件（Completed）走独立高优通道，保证不丢
- Engine 核心永远不阻塞

#### TUI 侧改造
```
Do/Input → 发送队列（异步，不阻塞 UI）→ 后台 goroutine → stdin
events channel → 环形缓冲（或直接丢 delta，靠 SessionLoaded 兜底）
```
- 发送操作入队即返回，UI goroutine 永远不卡
- 加心跳/ping 检测 Engine 存活，无响应时提示用户

#### 工具层截断
在工具执行层加结果截断（如 4MB），从源头避免大消息：
```go
if len(result) > maxToolResult {
    result = result[:maxToolResult] + "\n... [truncated]"
}
```

### 方案 B：应用层分片传输

大 payload 拆成多个 delta 事件，接收端拼接：
```
ToolExecStarted → ToolExecDelta × N → ToolExecCompleted
```
彻底绕开 8MB 限制，还能增强流式体验。

### 方案 C：换 Unix socket + 重连机制（长期方案）

从 stdio pipe 换成 Unix domain socket：
- Engine 检测到对端断开后进入"等待重连"模式，不退出
- TUI 重启后重连，发 LoadSession 同步状态
- 支持真正的"UI 崩溃 Engine 继续跑"

---

## 优先级

1. **P0**：TUI 写异步队列（防止 UI 卡死，改造成本最低）
2. **P0**：工具层结果截断（防止 8MB 炸通道）
3. **P1**：Engine 侧异步缓冲 + 事件分级（防止拖死 Engine）
4. **P2**：Unix socket 替换 stdio（长期架构升级）
