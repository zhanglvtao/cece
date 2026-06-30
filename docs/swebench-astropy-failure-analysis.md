# SWE-bench 失败案例深度分析

> 日期: 2026-06-29
> 模型: traecli/GPT-5.5, traecli/deepseek-v4-pro
> 数据集: SWE-bench_Lite (princeton-nlp)

## 总结

两个模型在 astropy 的 6 个 case 上表现**完全一致**：同样 3 个 PASS，同样 3 个 FAIL。失败案例的根本原因是模型**过早满足于第一个修复点**，没有系统性搜索是否还有其他需要同步修改的位置。

## 各模型在 astropy 上的表现

| # | Instance | GPT-5.5 | deepseek-v4-pro | 共同点 |
|---|----------|---------|----------------|--------|
| 0 | astropy__astropy-12907 | ✅ PASS | ✅ PASS | 简单，不用改代码 |
| 1 | astropy__astropy-14182 | ❌ FAIL | ❌ FAIL | incomplete fix |
| 2 | astropy__astropy-14365 | ❌ FAIL | ❌ FAIL | incomplete fix |
| 3 | astropy__astropy-14995 | ✅ PASS | ✅ PASS | 单一位置的修改 |
| 4 | astropy__astropy-6938 | ✅ PASS | ✅ PASS | 单一位置的修改 |
| 5 | astropy__astropy-7746 | ❌ FAIL | ❌ FAIL | incomplete fix |

非 astropy 的 4 个 django case 尚未对比（GPT-5.5 跑过但 deepseek 没跑）。

---

## 案例 1: astropy__astropy-14182 — RST header_rows

### 问题

RST 格式不支持 `header_rows` 参数。调用 `tbl.write(..., format="ascii.rst", header_rows=["name", "unit"])` 时抛出 `TypeError: RST.__init__() got an unexpected keyword argument 'header_rows'`。

### Gold Patch（官方修复）

需要修改 `astropy/io/ascii/rst.py` **3 处**：

1. **`SimpleRSTData`**: 删除 `start_line = 3` 硬编码（header_rows 动态变化时行数不固定）
2. **`RST.__init__`**: 增加 `header_rows=None` 参数传递给父类
3. **`RST.write`**: 重写分隔线插入逻辑，从硬编码 `lines[1]` 改为动态计算 header 行数

### 模型做了什么

| 轮次 | 模型 | 引擎时间 | 改了什么 |
|------|------|---------|---------|
| Jun 26 (首次) | GPT-5.5 | 93s | 只改了 `__init__`+ 参数传递 |
| Jun 27 (重跑) | GPT-5.5 | ~180s | 同上 |
| Jun 28 (重跑) | GPT-5.5 | ~180s | 同上 |
| Jun 29 (prompt fix) | GPT-5.5 | 120s | 改了 `__init__` + `write()`（用 `getattr`）+ **还改了测试文件** → conflict |
| Jun 29 (artifact filter) | GPT-5.5 | 161s | 同上，但过滤了测试文件修改；但 `write()` 实现过于取巧，没用 |
| Jun 29 (ds run) | deepseek-v4-pro | 360s | 改了 `__init__` + `SimpleRSTData.start_line` 动态化；但仍未正确实现 `write()` |

### 根因分析

模型能定位到 `RST.__init__` 这个入口点，但**没有跟踪 write 流水线**来理解 header_rows 参数在 write 时是怎么被使用的。write 方法中的分隔线插入逻辑 `lines = [lines[1]] + lines + [lines[1]]` 是硬编码的——model 没意识到这个硬编码在有多行 header 时会失效。

**修复难度**: 中等（需要理解 FixedWidth 的 write 继承层次）

---

## 案例 2: astropy__astropy-14365 — QDP warning

### 问题

`ascii.qdp` 假设 QDP 命令必须大写（如 "READ SERR 1 2"），但 QDP 本身不区分大小写。手写的小写命令会抛出 `ValueError: Unrecognized QDP line`。

### Gold Patch（官方修复）

修改 `astropy/io/ascii/qdp.py` **2 处**：

1. **第 71 行**: `re.compile(_type_re)` → `re.compile(_type_re, re.IGNORECASE)` — 使正则匹配命令时不区分大小写
2. **第 309 行**: `if v == "NO":` → `if v.upper() == "NO":` — 使数据值中的 "NO"（屏蔽值标记）也能不区分大小写

### 模型做了什么

| 轮次 | 模型 | 引擎时间 | 改了什么 |
|------|------|---------|---------|
| Jun 26 (首次) | GPT-5.5 | 113s | 只改了第 71 行（`re.IGNORECASE`） |
| Jun 29 (ds run) | deepseek-v4-pro | 63s | 同上 |

### 根因分析

模型找到了正则匹配的 case 修复点，但没继续搜索**同一文件中是否还有其他 case-sensitive 的比较**。第 309 行的 `v == "NO"` 与正则匹配在完全不同的代码路径（数据解析 vs 命令识别），模型没有系统地搜索所有受影响的比较。

**修复难度**: 低（加 `re.IGNORECASE` 和 `.upper()` 各一行）

---

## 案例 3: astropy__astropy-7746 — WCS zero-size

### 问题

向 WCS 变换函数传递空列表/数组时崩溃：
```python
wcs.wcs_pix2world([], [], 0)
# → InconsistentAxisTypesError: ncoord and/or nelem inconsistent with the wcsprm.
```

### Gold Patch（官方修复）

修改 `astropy/wcs/wcs.py` 中的 `_array_converter` 方法 **2 处**：

1. **`_return_list_of_arrays` 函数**: 当任意轴 `size==0` 时，直接返回 axes
2. **`_return_single_array` 函数**: 当 `xy.shape` 中有 `0` 时，直接返回 xy

### 模型做了什么

| 轮次 | 模型 | 引擎时间 | 改了什么 |
|------|------|---------|---------|
| Jun 26 (首次) | GPT-5.5 | 93s | 只改了 `_return_list_of_arrays` |
| Jun 29 (ds run) | deepseek-v4-pro | 74s | 同上 |

### 根因分析

测试调用了两种不同的入口：`wcs_pix2world([], [], 0)`（走 `_return_list_of_arrays`）和 `w.all_pix2world(np.zeros((0,2)), 0)`（走 `_return_single_array`）。模型只测试了第一个入口（直接用示例代码），没测试第二种形状。所以只改了 `_return_list_of_arrays`。

**修复难度**: 低（两处各加 3 行 guard clause）

---

## 跨案例模式总结

### 9 轮对话 vs 9 次 complete/fail 的关系

| Case | 总运行次数 | 不同的 patch | 通过 F2P | 说明 |
|------|-----------|-------------|---------|------|
| 14182 | 6 | 5 | 0 | 每个 patch 都不同，但没有一次触及所有需要改的位置 |
| 14365 | 2 | 2 | 0 | 每次都只改了正则，漏了 `v.upper()` |
| 7746 | 2 | 1 | 0 | 每次只改了一个 entry point |

### 共同根因: "过早满足综合征"（Premature Satisfaction）

所有 3 个失败案例呈现统一的失败模式：

```
模型定位到问题入口 → 找到一个修复点 → 修改后简单验证通过 → 提交完成
                                                      ↓
                                           遗漏了第 2、第 3 个修复点
```

**模型的能力足够修复这些 case**（gold patch 都是 2-5 行的改动），但它没有一个系统性习惯去问自己："这个问题还可能触及哪里？有没有其他类似模式需要同步修改？"

## Prompt 修改记录 (2026-06-30)

### 根因定位

问题不在模型能力不足——三个 case 里模型都读过所有相关代码（14365 读了整个
`qdp.py`，7746 读了整个 `_array_converter` 包括两个 entry point，14182 跟踪
了整个 FixedWidth 继承链）。失败是因为在"找到第一个修复点"与"验证完整覆盖"之间
断层。

**各层责任分配:**

| 层 | 文件 | 责任 |
|---|------|------|
| CC system.md | `internal/prompt/system.md` | **中等**。已有的 "don't stop at first passing symptom" 太模糊，没有转化为"搜索全篇"的可执行动作 |
| SWE-bench prompt | `benchmarks/prompts/swebench.py` | **高**。"Check nearby behavior" 不够具体，和具体任务的上下文脱节 |
| 模型本身 | — | **低**。14365/7746 的修复本身很简单，但 prompt 没有明确要求系统性搜索，模型按 prompt 执行其实没有明显违规 |

结论：**这是 prompt 的分层设计问题，不是模型能力问题。**

### 修改内容

#### 1. CC system.md（`internal/prompt/system.md`）

在 Coding Workflow 中插入了一条通用指导:

> After fixing one location, search the same module for other locations that
> manifest the same root cause before declaring done. "Same root cause" means:
> the same comparison pattern, the same missing guard, the same unchecked input
> path.

这条是通用编程习惯——不管什么任务，修完一个位置后应该查同一模块里其他同样
根因的位置。放在 system.md 的 Coding Workflow 段合适。

#### 2. SWE-bench benchmark prompt（`benchmarks/prompts/swebench.py`）

把第 7 步从模糊的 "Check nearby behavior" 替换为具体的 COMPLETENESS CHECK
三段式流程:

```markdown
7. COMPLETENESS CHECK — identify all affected locations:
   a. Use Grep to search the modified file(s) for ALL patterns that share
      the same root cause. If you fixed case sensitivity, search for other
      comparisons. If you added a guard, search for other code paths.
   b. Identify every entry point and argument form of the function(s) you
      modified. Verify each one independently.
   c. Only when all affected locations are addressed, proceed to step 8.
8. Use Bash to run `git diff` and review all your changes before finishing.
```

这条是任务特定指导——SWE-bench 的 issue 有固定的验证流程和特定的代码仓库
结构，适合在 benchmark prompt 中做精确控制。

### 两层的边界

- **system.md** 管通用编程习惯，所有场景都生效
- **benchmark prompt** 管任务上下文的精确控制，只在此 benchmark 中生效
- 这次修改遵循了这个分层：system.md 加一条通用原则，benchmark prompt 加具体检查清单