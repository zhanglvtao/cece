package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cece/internal/config"
	"cece/internal/tool"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Manager manages all MCP client connections and their tools.
type Manager struct {
	clients map[string]*Client
	configs config.MCPs
	tools   []tool.Tool
	mu      sync.Mutex
}

// ServerStatus describes the current state of an MCP server.
type ServerStatus struct {
	Name      string
	Type      config.MCPType
	Addr      string // URL for sse/streamable-http, command for stdio
	Connected bool
	ToolCount int
	Error     string
}

// NewManager creates a new MCP Manager.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*Client),
	}
}

// Initialize connects to all configured MCP servers and collects their tools.
// Failed connections are logged but do not block startup.
func (m *Manager) Initialize(ctx context.Context, configs config.MCPs) {
	if len(configs) == 0 {
		return
	}
	m.configs = configs

	type result struct {
		name   string
		client *Client
		tools  []tool.Tool
		err    error
	}

	ch := make(chan result, len(configs))
	for name, cfg := range configs {
		if cfg.Disabled {
			slog.Info("mcp disabled, skipping", "name", name)
			continue
		}
		go func(name string, cfg config.MCPConfig) {
			c := NewClient(name, cfg)
			if err := c.Connect(ctx); err != nil {
				ch <- result{name: name, err: err}
				return
			}
			mcpTools, err := c.ListTools(ctx)
			if err != nil {
				c.Close()
				ch <- result{name: name, err: err}
				return
			}
			var adapters []tool.Tool
			for _, t := range mcpTools {
				adapters = append(adapters, &mcpAdapter{
					client:   c,
					server:   name,
					toolName: t.Name,
					def:      convertToolDef(name, t),
				})
			}
			ch <- result{name: name, client: c, tools: adapters}
		}(name, cfg)
	}

	for range configs {
		r := <-ch
		if r.err != nil {
			slog.Warn("mcp init failed", "name", r.name, "error", r.err)
			continue
		}
		m.mu.Lock()
		m.clients[r.name] = r.client
		m.tools = append(m.tools, r.tools...)
		m.mu.Unlock()
		slog.Info("mcp tools loaded", "name", r.name, "count", len(r.tools))
	}
}

// Tools returns all MCP tools as tool.Tool interfaces.
func (m *Manager) Tools() []tool.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tool.Tool, len(m.tools))
	copy(out, m.tools)
	return out
}

// Status returns the status of all configured MCP servers.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []ServerStatus
	for name, cfg := range m.configs {
		s := ServerStatus{
			Name: name,
			Type: cfg.Type,
		}
		if cfg.Type == config.MCPStdio {
			s.Addr = cfg.Command
		} else {
			s.Addr = cfg.URL
		}
		if _, ok := m.clients[name]; ok {
			s.Connected = true
			// Count tools for this server
			count := 0
			for _, t := range m.tools {
				if adapter, ok := t.(*mcpAdapter); ok && adapter.server == name {
					count++
				}
			}
			s.ToolCount = count
		} else if cfg.Disabled {
			s.Error = "disabled"
		}
		statuses = append(statuses, s)
	}
	return statuses
}

// ConnectOne connects a single MCP server by name and registers its tools.
func (m *Manager) ConnectOne(ctx context.Context, name string) error {
	m.mu.Lock()
	cfg, ok := m.configs[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp %s: not found in config", name)
	}
	if _, exists := m.clients[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("mcp %s: already connected", name)
	}
	m.mu.Unlock()

	c := NewClient(name, cfg)
	if err := c.Connect(ctx); err != nil {
		return err
	}
	mcpTools, err := c.ListTools(ctx)
	if err != nil {
		c.Close()
		return err
	}
	var adapters []tool.Tool
	for _, t := range mcpTools {
		adapters = append(adapters, &mcpAdapter{
			client:   c,
			server:   name,
			toolName: t.Name,
			def:      convertToolDef(name, t),
		})
	}

	m.mu.Lock()
	m.clients[name] = c
	m.tools = append(m.tools, adapters...)
	m.mu.Unlock()
	slog.Info("mcp connected", "name", name, "tools", len(adapters))
	return nil
}

// DisconnectOne disconnects a single MCP server by name and removes its tools.
func (m *Manager) DisconnectOne(name string) error {
	m.mu.Lock()
	c, ok := m.clients[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp %s: not connected", name)
	}
	// Remove tools for this server
	filtered := make([]tool.Tool, 0, len(m.tools))
	for _, t := range m.tools {
		if adapter, ok := t.(*mcpAdapter); ok && adapter.server == name {
			continue
		}
		filtered = append(filtered, t)
	}
	m.tools = filtered
	delete(m.clients, name)
	m.mu.Unlock()

	if err := c.Close(); err != nil {
		slog.Warn("mcp close error", "name", name, "error", err)
	}
	slog.Info("mcp disconnected", "name", name)
	return nil
}

// Registry returns the tool.Registry hook for dynamic tool injection/removal.
// Used by EngineMediator to sync Registry with MCP state after connect/disconnect.
func (m *Manager) RegistryTools() []tool.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tool.Tool, len(m.tools))
	copy(out, m.tools)
	return out
}

// Close shuts down all MCP connections.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, c := range m.clients {
		if err := c.Close(); err != nil {
			slog.Warn("mcp close failed", "name", name, "error", err)
		}
	}
}

// mcpAdapter adapts an MCP tool to the tool.Tool interface.
type mcpAdapter struct {
	client   *Client
	server   string
	toolName string
	def      tool.Definition
}

func (a *mcpAdapter) Info() tool.Definition {
	return a.def
}

func (a *mcpAdapter) Run(ctx context.Context, input json.RawMessage, emitter tool.Emitter) tool.Result {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("calling mcp %s/%s...", a.server, a.toolName))
	}

	result, err := a.client.CallTool(ctx, a.toolName, args)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}
	}

	var textParts []string
	for _, content := range result.Content {
		if tc, ok := content.(*mcpsdk.TextContent); ok {
			textParts = append(textParts, tc.Text)
		} else {
			textParts = append(textParts, fmt.Sprintf("%v", content))
		}
	}

	content := strings.Join(textParts, "\n")
	return tool.Result{Content: content, IsError: result.IsError}
}

// convertToolDef converts an MCP Tool to a tool.Definition.
func convertToolDef(serverName string, t *mcpsdk.Tool) tool.Definition {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	if t.InputSchema != nil {
		// InputSchema from the SDK is already a map[string]any after JSON unmarshal
		if m, ok := t.InputSchema.(map[string]any); ok {
			schema = m
		}
	}

	desc := t.Description
	if desc == "" && t.Title != "" {
		desc = t.Title
	}

	return tool.Definition{
		Name:        fmt.Sprintf("mcp_%s_%s", serverName, t.Name),
		Description: desc,
		InputSchema: schema,
	}
}
