package testkit

import (
	"github.com/zhanglvtao/cece/internal/mcp"
)

// NewEmptyMCPManager returns a real *mcp.Manager with no servers
// configured. Useful for tests that exercise the /mcp slash command
// path (List/Connect/Disconnect) without real MCP processes.
//
// For tests that need to inject "MCP-style" tools (i.e. with mcp_*
// names) into the registry, use WithExtraTools instead — the engine
// treats any registered tool the same regardless of source.
//
// A full FakeMCPManager would require introducing an interface in
// internal/mcp; we deliberately stay out of that refactor here.
func NewEmptyMCPManager() *mcp.Manager {
	return mcp.NewManager()
}
