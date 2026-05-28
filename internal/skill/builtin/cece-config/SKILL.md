---
name: cece-config
description: 帮助用户配置 Cece — 设置 providers、模型、权限、工具等
---

# Cece Configuration

Cece 使用 JSON 配置文件，路径优先级（从高到低）：

1. `.cece/settings.json`（项目级）
2. `~/.cece/settings.json`（全局）

## 配置结构

```json
{
  "provider": {
    "model": "claude-sonnet-4-6",
    "maxTokens": 16384,
    "providers": []
  },
  "debug": { "enabled": false },
  "yolo": { "enabled": false },
  "tool_result": {
    "inline_max_lines": 200,
    "head_lines": 80,
    "tail_lines": 80
  }
}
```

## Provider 配置

每个 provider 包含：

- `name`: 标识符
- `protocol`: `"anthropic"`（默认）或 `"openai"` 或 `"codebase"`
- `apiKey`: API 密钥
- `baseURL`: 自定义端点
- `authMode`: `"apikey"`（默认）或 `"bearer"`
- `authHelper`: Shell 命令获取动态 token
- `models`: 静态模型列表（不支持 /v1/models 的 provider 使用）

## 环境变量覆盖

- `ANTHROPIC_API_KEY`: 自动创建 env provider
- `ANTHROPIC_BASE_URL`: 覆盖 API 端点
- `ANTHROPIC_MODEL`: 覆盖默认模型
- `ANTHROPIC_MAX_TOKENS`: 覆盖 max tokens
- `ZLAUDE_YOLO=1`: 启用自动审批模式

## 注意事项

- 修改配置后需要重启 cece 才能生效
- API 密钥不要提交到 git，建议使用环境变量或 authHelper
