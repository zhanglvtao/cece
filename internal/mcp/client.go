package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"cece/internal/config"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultTimeout = 15

// Client wraps an MCP client session for a single MCP server.
type Client struct {
	name    string
	cfg     config.MCPConfig
	client  *mcpsdk.Client
	session *mcpsdk.ClientSession
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// NewClient creates a new MCP client for the given server name and config.
func NewClient(name string, cfg config.MCPConfig) *Client {
	return &Client{
		name: name,
		cfg:  cfg,
	}
}

// Connect establishes a connection to the MCP server.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	transport, err := c.createTransport()
	if err != nil {
		return fmt.Errorf("mcp %s: create transport: %w", c.name, err)
	}

	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	c.cancel = cancel

	impl := &mcpsdk.Implementation{
		Name:    "cece",
		Version: "0.1.0",
	}
	client := mcpsdk.NewClient(impl, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("mcp %s: connect: %w", c.name, err)
	}

	c.client = client
	c.session = session
	slog.Info("mcp connected", "name", c.name)
	return nil
}

// ListTools returns the tools available on the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]*mcpsdk.Tool, error) {
	c.mu.Lock()
	session := c.session
	c.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("mcp %s: not connected", c.name)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: list tools: %w", c.name, err)
	}
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (*mcpsdk.CallToolResult, error) {
	c.mu.Lock()
	session := c.session
	c.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("mcp %s: not connected", c.name)
	}

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp %s: call tool %s: %w", c.name, toolName, err)
	}
	return result, nil
}

// Close shuts down the MCP client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	session := c.session
	cancel := c.cancel
	c.session = nil
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if session != nil {
		return session.Close()
	}
	return nil
}

func (c *Client) createTransport() (mcpsdk.Transport, error) {
	switch c.cfg.Type {
	case config.MCPStreamableHTTP:
		return c.createStreamableHTTPTransport()
	case config.MCPsse:
		return c.createSSETransport()
	case config.MCPStdio:
		return c.createStdioTransport()
	default:
		return nil, fmt.Errorf("unsupported mcp type: %s", c.cfg.Type)
	}
}

func (c *Client) createStreamableHTTPTransport() (*mcpsdk.StreamableClientTransport, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("streamable-http requires a url")
	}
	httpClient := &http.Client{
		Transport: c.headerTransport(),
	}
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   c.cfg.URL,
		HTTPClient: httpClient,
	}, nil
}

func (c *Client) createSSETransport() (*mcpsdk.SSEClientTransport, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("sse requires a url")
	}
	httpClient := &http.Client{
		Transport: c.headerTransport(),
	}
	return &mcpsdk.SSEClientTransport{
		Endpoint:   c.cfg.URL,
		HTTPClient: httpClient,
	}, nil
}

func (c *Client) createStdioTransport() (*mcpsdk.CommandTransport, error) {
	if c.cfg.Command == "" {
		return nil, fmt.Errorf("stdio requires a command")
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	envs := os.Environ()
	for k, v := range c.cfg.Env {
		envs = append(envs, k+"="+v)
	}
	cmd.Env = envs
	return &mcpsdk.CommandTransport{
		Command: cmd,
	}, nil
}

func (c *Client) headerTransport() *headerRoundTripper {
	return &headerRoundTripper{headers: c.cfg.Headers}
}

type headerRoundTripper struct {
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range rt.headers {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}
