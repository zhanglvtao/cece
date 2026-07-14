# Issue 005: SWE-bench 评分应使用官方库而非自维护副本

## 问题描述

当前 `benchmarks/scorers/swebench_specs.py` 是一份自维护的官方 SWE-bench 测试规格副本，包含：

- 各 repo 的测试命令（`MAP_REPO_TO_TEST_CMD`）
- 6 个日志解析器（pytest / django / sympy / seaborn 等）
- resolution 计算逻辑（`compute_resolution`）

相关代码：
- 自维护规格：[swebench_specs.py:1-270](file:///Users/bytedance/cece/benchmarks/scorers/swebench_specs.py)
- 评分入口：[swebench.py:219-354](file:///Users/bytedance/cece/benchmarks/scorers/swebench.py)
- 依赖声明：[requirements-swebench.txt:1](file:///Users/bytedance/cece/requirements-swebench.txt)（仅 `datasets`）

## 根本原因

注释里写的原因是：

> *"the official `swebench` package requires Python 3.10+ (PEP 604 syntax) and is unusable on the Python 3.9 host"*

但评分是在 Docker 容器里执行的，容器内可以装任意 Python 版本，不依赖宿主机。所以这个理由不成立。

## 风险

- **偏离官方**：自维护副本可能与官方 SWE-bench 评分逻辑产生偏差，导致 benchmark 结果不可比
- **维护成本**：官方更新测试命令或解析器后，需要手动同步
- **功能缺失**：官方包可能提供额外的校验、报告、统计功能

## 修复建议

1. 在 Docker 镜像中安装官方 `swebench` 包（Python 3.10+）
2. 评分时调用 `swebench.harness.run_evaluation` 或等价 API
3. 也可直接调用官方的 `swebench.harness.log_parsers` 和 `swebench.harness.constants` 替代自维护常量
4. 删除 `swebench_specs.py`
5. `requirements-swebench.txt` 中加入 `swebench`

## 优先级

**P2**：不影响功能正确性，但影响 benchmark 结果的权威性和可维护性。