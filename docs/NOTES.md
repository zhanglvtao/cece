# CC 开发过程中遇到的问题

## Edit 工具 Tab 匹配问题（2026-06-03 已修复）

### 问题现象
Edit 工具调用时，如果文件使用 tab 缩进（如 Go 源码），LLM 构造的 `old_string` 经常无法匹配文件中的实际内容，导致编辑失败。

### 根因

**1. Read 工具输出混淆**

Read 工具输出格式为 `行号\t内容`，如果文件内容本身包含 tab（缩进），LLM 看到的输出中同时存在「展示用 tab」和「内容用 tab」，视觉上无法区分。LLM 构造 `old_string` 时可能多/少 tab。

修复：`read.go` 行号后分隔符从 `\t` 改为 `| `。

**2. Edit 工具双向标准化的架构缺陷**

旧实现 `findActualString` 对 file content 和 old_string **双向**做标准化（CRLF、tab/space、trailing ws、quotes），每次标准化都复制整个文件内容。这有两个问题：
- **性能差**：对大文件做 4 次 `strings.ReplaceAll` 复制
- **语义错误**：应该只变换 `old_string`（LLM 输出），适配原文件内容去搜索，而不是把文件内容也做变换

修复：重构为**单向变换**——只生成 `old_string` 的候选变体（tab↔spaces、CRLF↔LF、弯引号↔直引号），用 `strings.Index` 在原文件内容中直接搜索（零拷贝）。

### 参考
- opencode 的 `Replacer` 模式：只变换 old_string，逐行比较，零拷贝
- crush 的简单策略：只做 CRLF→LF，不做 fuzzy matching

---

## Edit 工具前导特殊字符匹配问题

### 问题现象
Edit 工具的 `old_string` 参数需要精确匹配文件内容，但 LLM 经常在 `old_string` 的前导空白（tab/space 混合）上出错。

### 根因
LLM 的 tokenization 对空白字符不敏感，特别是：
- Tab 和空格的混合缩进容易搞混
- 前导空格数量经常不对（多了或少了）
- 行尾空白容易被 LLM 忽略

### 缓解
Edit 工具的 fuzzy matching cascade 逐步尝试多种标准化策略，增加容错。但更好的方案是从源头减少歧义（如 Read 输出改用无歧义分隔符）。
