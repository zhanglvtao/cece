# CC 开发过程中的问题记录

## 1. 事件流式（Event Streaming）相关

### cece engine JSONL IPC 通信
- cece engine 通过 stdin/stdout 的 JSONL 协议通信
- 事件类型: `engine_ready`, `mode_changed`, `turn_completed`, `run_failed` 等
- 如果进程在启动后没有产生任何事件就退出，需要读取 stderr 来诊断
- `run_until_done` 需要设置合理的 timeout，否则可能永远阻塞

### Python 输出缓冲问题
- `python3 -m benchmarks` 的 print 输出在后台运行（nohup/重定向）时会被缓冲
- 必须使用 `python3 -u` 或 `PYTHONUNBUFFERED=1` 或 `print(..., flush=True)`
- nohup 在 macOS 上行为不稳定，进程可能被 kill；使用 `tmux` 更可靠

## 2. 工具实现相关

### Edit 工具的前导特殊字符匹配
- Edit 工具依赖精确的字符串匹配（old_string），文件中的特殊 Unicode 字符、
  不可见字符、不同的缩进风格都会导致匹配失败
- 没有行号锚定，纯靠内容匹配——对大文件中重复模式不友好

### Docker exec + subprocess.run + capture_output 超时
- `subprocess.run(cmd, capture_output=True, timeout=N)` 超时后会 SIGKILL 子进程
- 但 `docker exec` 被 kill 后，容器内的命令可能还在运行
- `rc=-9` 表示被 SIGKILL，通常是 subprocess 超时导致的
- 解决方案：把长时间命令拆成多个短步骤，每步有独立超时

### Completion gate 固定重试上限
- Completion gate 之前把收尾补救限制死在 3 次，超过后直接 `force_complete`
- 这会把“模型还可以继续自救”的场景过早截断，UI 只看到 `partial closure`
- 更合理的方式是持续注入 gate reminder，让 LLM 自己调用 `UpdateTaskClosure` 明确结束（如 `blocked` / `not_needed`），而不是靠固定次数兜底

### Completion gate 无进展空转
- 去掉固定上限后，如果模型连续只输出普通文本、不调用工具，就可能在 completion gate 上空转
- 第一版保护策略：连续 2 次 completion_gate 无工具动作后，升级为更强的 reminder，明确禁止 plain text，要求必须调用 `UpdateTaskClosure` / `Todo` / `AskUserQuestion` / `ExitPlanMode`
- 先不自动替模型结束任务，避免错误闭环；只做强制收尾动作提示

## 3. SWE-bench 特定问题

### Git clone 策略
- `git clone --depth 1` + `git fetch origin <commit>` 比 `git fetch --unshallow` 快得多
- 对于大仓库（astropy），unshallow 需要拉取整个历史，可能要几分钟
- `git fetch origin <commit>` 只拉取单个 commit 的对象，通常几秒到几十秒
- 如果 fetch 单个 commit 失败（shallow clone 不支持），再 fallback 到 unshallow

### Python 包编译
- astropy 需要 C 扩展编译（`python setup.py develop`），在 arm64 Docker 里每次约 60-90s
- 需要 `setuptools<58`（新版本移除了 `dep_util`）、`cython`、`numpy<2`
- `pip install -e .` 会触发 build isolation，使用系统 setuptools 而非已安装的旧版
- `python setup.py develop` 直接用当前环境的 setuptools，避免 build isolation 问题

### 模型名映射
- 用户说"glm-5"，但 Aiden API 上的正确名称是 `glm-5.1`
- 可用模型：`glm-5.1`, `glm-5v`, `gpt-5.5-paygo` 等
- 需要通过 Aiden API `/v1/models` 端点查询可用模型

### FAIL_TO_PASS 格式
- SWE-bench 数据集中的 `FAIL_TO_PASS` 字段是 JSON 字符串，不是 list
- 需要 `json.loads(fail_to_pass_raw)` 解析

### Django 测试
- Django 不用 pytest，用 `python manage.py test module.Class.test_name`
- 测试标识符格式：`test_name (module.Class)`

## 4. 评分器（Scorer）问题

### git clean 删除 .cece 目录
- `git clean -fd` 会删除 `.cece/` 下的配置文件
- 解决方案：`git stash --include-untracked` + `git clean -fd -- . ':!.cece'`

### "no tests ran" 误判
- pytest 输出 "no tests ran" 表示测试没找到，不应算 pass
- 需要显式检查 "no tests ran" 并返回 False
