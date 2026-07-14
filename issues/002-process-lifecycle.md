# Issue 002: TUI 进程退出导致 Engine 子进程跟随退出

## 问题描述

当前设计下，TUI 进程退出（正常退出、崩溃、被 kill）会导致 Engine 子进程也跟着退出。

这与文档中"UI 崩溃不丢 Agent 状态"的描述容易产生误解：
- ✅ **状态数据不丢**：Engine 退出前会话已持久化到 `.cece/`，TUI 重启可通过 LoadSession 恢复
- ❌ **Engine 进程不会继续运行**：TUI 一退，Engine 也跟着退，长任务会被打断

## 根本原因

TUI 是父进程，Engine 是子进程，通过 stdio pipe 通信：

```
用户运行 `cece` → TUI 进程（父）
    ↓ exec.Command("cece", "engine", "--stdio")
  Engine 子进程
```

当 TUI 进程退出时：
1. OS 自动关闭 TUI 持有的所有文件描述符
2. stdin pipe 的写端被关闭
3. Engine 端 `scanner.Scan()` 读到 EOF，返回 false
4. Engine 执行 cleanup（cancel ctx → wg.Wait → runtime.Wait）后正常退出

相关代码：[stdio_engine.go:69-101](file:///Users/bytedance/cece/internal/ipc/stdio_engine.go#L69-L101)

```go
for scanner.Scan() {
    // 处理消息...
}
// scanner 返回 false → 跳出循环
cancel()
wg.Wait()
runtime.Wait()
return nil // Engine 退出
```

## 影响范围

- TUI 崩溃 = Engine 停止，Agent 执行中断
- 长耗时任务（如大重构、多轮工具调用）可能被打断
- 无法实现"TUI 断开后 Engine 继续后台执行，之后重连查看结果"的体验
- 进程分离的故障隔离效果打折扣

## 什么时候 Engine "可能"继续跑？

只有一种情况：**TUI 进程没死透，只是 UI 层崩溃但进程还在，stdin 写端未关闭**。

比如 Bubble Tea 渲染 panic 被 recover，但主进程存活。这种情况下 Engine 不会收到 EOF，会继续跑。但这是偶然情况，不是设计保证。

## 修复建议

按工程复杂度排序：

### 方案 A（推荐，长期架构升级）：Unix Domain Socket 替换 stdio

用 Unix socket 替代 stdio pipe 通信：

```
TUI 进程（父）
    ↓ 创建 socket 并启动 Engine
Engine 进程（子）→ 连接 socket
    ↓
TUI 崩了 → socket 断开
    ↓
Engine 检测到断开 → 进入"等待重连"模式 → 继续跑 Agent
    ↓
TUI 重启 → 重新连接 socket → 发 LoadSession/增量同步 → 恢复展示
```

优点：
- Engine 可以区分"对端暂时断开"和"应该退出"
- 支持重连，状态无缝恢复
- 比 daemon 模式简单，不需要管理 pid/socket 文件生命周期

需要改造点：
- IPC 层抽象出 Transport 接口（stdio/socket 可切换）
- Engine 增加"等待重连"状态机
- TUI 增加重连逻辑和状态同步

### 方案 B：Engine 独立 daemon 模式（最彻底）

Engine 作为独立 daemon 进程运行，TUI 只是客户端：

```bash
# 先启动 Engine daemon
cece engine --daemon --socket /tmp/cece-<project>.sock
# TUI 连接上去
cece --connect /tmp/cece-<project>.sock
```

优点：
- 生命周期完全解耦，TUI 和 Engine 互不影响
- 多个 TUI 可以连同一个 Engine（虽然当前不需要）
- 支持后台执行、重连查看等高级特性

缺点：
- 需要处理 daemon 管理（启动/停止/状态查询）
- 需要处理 socket 文件权限、清理
- 需要考虑多实例隔离

### 方案 C：Detach 信号 + 优雅断开（折中，快速实现）

TUI 优雅退出前发一个 `DetachAction`，告诉 Engine "我主动断开，你继续跑"。Engine 收到后：
1. 关闭 stdin 读循环
2. 进入 detached 模式，事件暂存到内存/磁盘
3. 等新 TUI 连接后再继续输出

缺点：
- 处理不了 TUI 异常崩溃（没机会发 DetachAction）
- 需要额外的重连机制（还是绕不开 socket）

### 方案 D：stdio 双工 + 进程组设置（不可行，说明一下）

有人可能想"设置进程组让 Engine 不随 TUI 退出"，但这解决不了问题——stdin 写端关闭后 Engine 还是会读到 EOF 退出，而且会变成孤儿进程没人管。

## 优先级

- **P2**：这是架构级升级，不影响核心功能，但影响可靠性天花板
- 建议在 IPC 层抽象 Transport 接口后，先做 stdio 实现，再逐步加 socket 实现
