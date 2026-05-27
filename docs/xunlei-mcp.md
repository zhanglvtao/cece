# 迅雷下载 MCP 服务器

## 概览

- **服务器名称**: `mcp-tool-server`
- **版本**: `1.0.0`
- **协议**: MCP (Model Context Protocol) over SSE + JSON-RPC 2.0
- **端点**: `https://api-xmodels.xunlei.com/models/sse/<API_KEY>`

该 MCP 服务器将迅雷下载能力暴露为标准 MCP 工具，允许 AI 模型通过协议调用完成下载链接校验、创建下载任务、查看任务列表、操作任务等操作。

## 连接方式

### 1. 建立 SSE 连接

```
GET https://api-xmodels.xunlei.com/models/sse/<API_KEY>
```

返回 SSE 事件，包含本次会话的消息端点：

```
event: endpoint
data: /models/message?sessionId=<SESSION_ID>&key=<API_KEY>
```

### 2. 初始化握手

向消息端点发送 `initialize` 请求：

```json
POST /models/message?sessionId=<SESSION_ID>&key=<API_KEY>

{
  "jsonrpc": "2.0",
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": { "name": "your-client", "version": "1.0" }
  },
  "id": 1
}
```

响应（通过 SSE 流返回）：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "capabilities": { "tools": {} },
    "serverInfo": { "name": "mcp-tool-server", "version": "1.0.0" }
  }
}
```

### 3. 发送初始化完成通知

```json
POST /models/message?sessionId=<SESSION_ID>&key=<API_KEY>

{
  "jsonrpc": "2.0",
  "method": "notifications/initialized"
}
```

### 4. 调用工具

所有工具调用通过 `tools/call` 方法发起，响应通过 SSE 流异步返回。

请求格式：

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "<工具名>",
    "arguments": { ... }
  },
  "id": <递增ID>
}
```

## 工具列表

### xunlei_download_list_device

获取可用下载设备列表。

**调用时机**: 创建或查看任务前必须先调用，获取有效的 `target` 值。

**参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `page_size` | number | 否 | 10 | 每页数据量 |
| `page_token` | string | 否 | "" | 翻页标识，首次不传 |

**返回示例**:

```json
{
  "next_page_token": "",
  "device": [
    {
      "name": "绿联-UGREEN-C4C7-135111",
      "target": "device_id#4a1892cb34ee117cab75259d6d595dc3",
      "product_name": "绿联"
    }
  ]
}
```

**分页规则**:
1. 首次调用不传 `page_token`
2. 若返回包含 `next_page_token`，继续调用获取下一页
3. 直到 `next_page_token` 为空，表示已获取全部设备
4. 所有页面获取完毕后仍无设备时，应提示用户检查迅雷是否运行

---

### xunlei_download_check_urls

校验下载链接是否有效。

**调用时机**: 创建下载任务前必须先调用，确认链接可用。

**参数**:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `urls` | string[] | 是 | 下载链接数组 |

**支持的链接协议**:
- HTTP / HTTPS
- FTP
- 磁力链（magnet://）
- 迅雷专用链（thunder://）
- ed2k 链接

**调用示例**:

```json
{
  "name": "xunlei_download_check_urls",
  "arguments": {
    "urls": ["magnet:?xt=urn:btih:example", "https://example.com/file.zip"]
  }
}
```

---

### xunlei_download_create

在指定设备上创建下载任务。

**前置条件**:
1. 通过 `xunlei_download_list_device` 获取 `target`
2. 通过 `xunlei_download_check_urls` 校验链接有效性

**参数**:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `target` | string | 是 | 目标设备 ID，必须从 list_device 获取，严禁编造 |
| `urls` | string[] | 是 | 下载链接数组 |
| `names` | string[] | 否 | 任务名称数组，长度须与 urls 一致；用户未要求命名时不传 |

**调用示例**:

```json
{
  "name": "xunlei_download_create",
  "arguments": {
    "target": "device_id#4a1892cb34ee117cab75259d6d595dc3",
    "urls": ["https://example.com/file.zip"],
    "names": ["示例文件"]
  }
}
```

**注意**:
- 严禁单次请求中多次调用本工具，除非用户明确要求
- 若无 target，应提示"无可用下载设备，请检查迅雷"

---

### xunlei_download_list

查看指定设备上的下载任务列表。

**前置条件**: 通过 `xunlei_download_list_device` 获取 `target`。

**参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `target` | string | 是 | - | 目标设备 ID |
| `page_size` | number | 否 | 10 | 每页数据量 |
| `page_token` | string | 否 | "" | 翻页标识 |

**分页规则**: 同 `list_device`，必须获取所有页面数据后才能提供完整列表。

**任务状态**:
- "等待中" 和 "进行中" 均视为"正在下载"

---

### xunlei_download_operate

操作下载任务（开始/暂停/删除）。

**前置条件**:
1. 通过 `xunlei_download_list_device` 获取 `target`
2. 通过 `xunlei_download_list` 或 `xunlei_download_create` 获取 `task_id`

**参数**:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `target` | string | 是 | 目标设备 ID |
| `task_id` | string | 是 | 任务 ID |
| `action` | string | 是 | 操作类型：`running`（开始/恢复）、`pause`（暂停）、`delete`（删除） |

**调用示例**:

```json
{
  "name": "xunlei_download_operate",
  "arguments": {
    "target": "device_id#4a1892cb34ee117cab75259d6d595dc3",
    "task_id": "task_12345",
    "action": "pause"
  }
}
```

**注意**:
- `delete` 操作不可恢复，执行前应确认用户意图
- 同一任务在单次对话中只能执行一次相同操作

## 典型工作流

```
1. xunlei_download_list_device  →  获取 target（设备 ID）
2. xunlei_download_check_urls   →  校验下载链接有效性
3. xunlei_download_create       →  创建下载任务（target + 有效链接）
4. xunlei_download_list         →  查看任务进度
5. xunlei_download_operate      →  开始/暂停/删除任务
```

## 协议细节

### 通信模型

- **控制通道**: POST `/models/message?sessionId=...&key=...`（发送 JSON-RPC 请求）
- **数据通道**: GET SSE 流（接收 JSON-RPC 响应和通知）
- 两者通过 `sessionId` 关联，SSE 连接断开后 session 失效

### 心跳

服务器会定期发送 ping 事件：

```
data:{"jsonrpc":"2.0","id":1,"method":"ping"}
```

客户端应保持 SSE 连接活跃，断开后需重新建立连接获取新的 sessionId。

### 错误码

| 错误码 | 含义 |
|--------|------|
| `-32600` | Invalid JSON-RPC version（请求格式不正确） |
| `-32601` | Method not found（方法名不存在） |
| `-32602` | Invalid session ID（session 过期或不匹配） |
