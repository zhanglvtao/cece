# cece

A terminal-based AI coding agent powered by Anthropic Claude.

Cece runs in your terminal, reads your codebase, and helps you write, edit, debug, and understand code — with full tool access (file read/write, grep, glob, bash, web fetch) and a minimal, keyboard-driven TUI.

## Features

- **Code-aware**: Reads your project, understands context, edits files directly
- **Tool suite**: Read, Edit, Write, Bash, Grep, Glob, WebFetch, AskUser, and more
- **MCP support**: Connect external tools via Model Context Protocol (stdio / SSE / streamable-http)
- **Skills**: Extensible prompt templates for repetitive workflows
- **Session persistence**: Resume conversations across restarts
- **Three modes**: default (confirm writes), auto-accept (yolo), plan (review before execute)
- **Context management**: Auto-compact at threshold, manual `/compact`, `/truncate-tool-result`
- **Minimal TUI**: Status bar, slash commands, vim-like chat scrolling, file picker

## Requirements

- **Go >= 1.24**
- **Anthropic API key** (or any OpenAI-compatible endpoint)

## Quick Start

```bash
git clone https://github.com/zhanglvtao/cece.git
cd cece
./install.sh
```

The script checks Go, builds the binary, installs it to PATH, and reminds you to set up a config file.

## Build from Source

```bash
git clone https://github.com/zhanglvtao/cece.git
cd cece
go build -o cece ./cmd/cece
```

## Configuration

Cece looks for settings in two locations (project-level overrides user-level):

| Location | Scope |
|---|---|
| `.cece/settings.json` | Project-level (checked into repo or per-project) |
| `~/.cece/settings.json` | User-level (global default) |

### Quick Start

```bash
mkdir -p ~/.cece
cp docs/settings.example.json ~/.cece/settings.json
# Edit apiKey in ~/.cece/settings.json
```

### Config Template

A ready-to-use template with all fields and comments: [`docs/settings.example.json`](docs/settings.example.json)

### Field Reference

**provider** section:

| Field | Default | Description |
|---|---|---|
| `model` | `claude-sonnet-4-6` | Default model ID |
| `maxTokens` | `16384` | Max output tokens per request |
| `modelContextMapping` | — | Override context window per model (e.g. `{"claude-sonnet-4-6": 200000}`) |
| `providers[]` | required | List of API providers |

**provider.providers[]** fields:

| Field | Default | Description |
|---|---|---|
| `name` | required | Provider identifier |
| `protocol` | `anthropic` | `anthropic`, `codebase`, or `aiden` |
| `apiKey` | — | Static API key |
| `baseURL` | — | API endpoint URL |
| `authMode` | `apikey` | `apikey` or `bearer` |
| `authHelper` | — | Shell command to fetch dynamic token |
| `models[]` | — | Static model list (for providers without /v1/models) |

**Other sections:**

| Field | Default | Description |
|---|---|---|
| `defaultMode.mode` | `default` | `default`, `auto-accept`, or `plan` |
| `debug.enabled` | `false` | Enable debug logging to `.cece/cece.log` |
| `yolo.enabled` | `false` | Auto-accept all tool calls |
| `tool_result.inline_max_lines` | `200` | Max lines for inline tool output |
| `tool_result.head_lines` | `80` | Head lines when truncated |
| `tool_result.tail_lines` | `80` | Tail lines when truncated |
| `mcp` | `{}` | MCP server connections (see [MCP](#mcp-model-context-protocol)) |

### Environment Variables

You can also configure via environment variables (no config file needed):

```bash
export ANTHROPIC_API_KEY="sk-ant-xxxxx"
export ANTHROPIC_BASE_URL="https://api.anthropic.com"   # optional
export ANTHROPIC_MODEL="claude-sonnet-4-6"              # optional
export ZLAUDE_YOLO="1"                                   # enable auto-accept mode
```

### Provider Protocols

| Protocol | Description |
|---|---|
| `anthropic` | Default. Direct Anthropic API or compatible proxy. |
| `codebase` | Codebase API with `configName` support. |
| `aiden` | Aiden protocol. |

### Auth Modes

| Mode | Description |
|---|---|
| `apikey` | Static API key in `apiKey` field (default) |
| `bearer` | Bearer token authentication |

`authHelper`: Shell command to dynamically fetch a token. When set, cece runs this command before each request and uses its stdout as the auth token.

## Usage

```bash
cd your-project
cece
```

### Modes

| Mode | Symbol | Behavior |
|---|---|---|
| **default** | ○ | Confirms before file writes and shell commands |
| **auto-accept** | ✓ | Auto-approves all tool calls (yolo mode) |
| **plan** | ✎ | LLM writes a plan first; you review before execution |

Switch modes with `Shift+Tab`.

## Key Bindings

### Editor (input area)

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+J` / `Shift+Enter` | Insert newline |
| `↑` / `↓` | Navigate input history |
| `Tab` | Autocomplete (slash commands, file paths) |
| `Esc` | Cancel / close popup |

### Chat (scroll area)

| Key | Action |
|---|---|
| `↑` / `k` | Scroll up |
| `↓` / `j` | Scroll down |
| `b` / `PgUp` | Page up |
| `f` / `PgDn` | Page down |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `Space` / `Enter` | Expand/collapse tool result |

### Global

| Key | Action |
|---|---|
| `Ctrl+C` | Quit |
| `Ctrl+G` | Toggle help |
| `Ctrl+O` | Switch focus between editor and chat |
| `Shift+Tab` | Cycle permission mode (default → auto-accept → plan) |
| `/` | Open slash command picker |

## Slash Commands

Type `/` in the input box to open the command picker:

| Command | Description |
|---|---|
| `/model` | Switch model |
| `/resume` | Resume a previous session |
| `/clear` | Clear conversation history |
| `/compact` | Compress conversation context (LLM-driven summary) |
| `/truncate-tool-result` | Truncate all inline tool results |
| `/dryrun` | Preview the full request before sending |
| `/skills` | List available skills |
| `/mcp` | Manage MCP servers |
| `/tool` | List registered tools |

## Status Bar

The status bar at the bottom shows real-time session info:

```
default ○ | claude-sonnet-4-6 | ctx:150K/200K 75% | in/out/cache:12K/3K/8K 67% | calls:5 | Read:3 Edit:2 Bash:1 | scroll:30%
```

| Section | Meaning |
|---|---|
| `default ○` | Current permission mode and symbol |
| `claude-sonnet-4-6` | Active model |
| `ctx:150K/200K 75%` | Remaining context / total window, usage percentage |
| `in/out/cache:12K/3K/8K 67%` | Input / output / cache-read tokens, cache hit rate |
| `calls:5` | Total API calls in this session |
| `Read:3 Edit:2 Bash:1` | Per-tool invocation counts |
| `scroll:30%` | Viewport scroll position (hidden when at bottom) |

## Built-in Tools

| Tool | Effect | Description |
|---|---|---|
| `Read` | read | Read file contents with line numbers |
| `Edit` | write | Make precise string replacements in files |
| `Write` | write | Create or overwrite a file |
| `Bash` | exec | Execute a shell command |
| `Grep` | read | Search file contents by regex |
| `Glob` | read | Search files by name pattern |
| `WebFetch` | read | Fetch URL content as markdown |
| `AskUser` | — | Ask the user a question |
| `Task` | — | Track progress on multi-step tasks |
| `Compact` | — | Compress conversation context |
| `EnterPlanMode` | mode | Enter plan mode |
| `ExitPlanMode` | mode | Exit plan mode (with plan approval) |

## MCP (Model Context Protocol)

Connect external tool servers to extend cece's capabilities.

### stdio example (local process)

```json
{
  "mcp": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/projects"],
      "timeout": 15
    }
  }
}
```

### SSE / streamable-http example (remote server)

```json
{
  "mcp": {
    "my-api": {
      "type": "sse",
      "url": "https://mcp.example.com/sse",
      "headers": {
        "Authorization": "Bearer my-token"
      }
    }
  }
}
```

MCP tools appear prefixed with `mcp_<server>_<tool>` (e.g. `mcp_filesystem_read_file`). Use `/mcp` to check server status and `/tool` to list all available tools.

## Project Layout

```
cmd/cece/         Entry point
internal/
  agent/           LLM client abstraction
  config/          Settings loading & merging
  engine/          Core orchestration loop
  mcp/             MCP client & manager
  prompt/          System prompt assembly
  protocol/        Event & action types
  session/         Session persistence
  skill/           Skill store
  tool/            Built-in tools
  ui/              TUI (bubbletea model, statusbar, picker)
```

## License

MIT
