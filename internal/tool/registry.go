package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Registry manages registered tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a Registry with the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Info().Name] = t
	}
	return r
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Register adds a tool to the registry at runtime.
func (r *Registry) Register(t Tool) {
	r.tools[t.Info().Name] = t
}

// SetMCPTools replaces all MCP tools in the registry.
// It removes any tool whose name starts with "mcp_", then adds the given tools.
func (r *Registry) SetMCPTools(tools []Tool) {
	// Remove existing MCP tools
	for name := range r.tools {
		if strings.HasPrefix(name, "mcp_") {
			delete(r.tools, name)
		}
	}
	// Add new MCP tools
	for _, t := range tools {
		r.tools[t.Info().Name] = t
	}
}

// Definitions returns all tool definitions for the API request.
func (r *Registry) Definitions() []Definition {
	defs := make([]Definition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Info())
	}
	return defs
}

// Execute runs a tool by name with the given input and emitter.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage, emitter Emitter) Result {
	t, ok := r.tools[name]
	if !ok {
		return Result{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}
	if verr := validateInput(t.Info(), input); verr != nil {
		return *verr
	}
	return t.Run(ctx, input, emitter)
}

// validateInput checks that all required fields in the tool's InputSchema
// are present in the input JSON. Returns nil on success.
func validateInput(def Definition, input json.RawMessage) *Result {
	required := getRequiredFields(def.InputSchema)
	if len(required) == 0 {
		return nil
	}

	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		msg := fmt.Sprintf("Invalid tool input JSON for %s: %v\nPlease provide a valid JSON object with the required parameters.", def.Name, err)
		return &Result{Content: msg, IsError: true}
	}

	props, _ := def.InputSchema["properties"].(map[string]any)

	var missing []string
	for _, field := range required {
		_, exists := params[field]
		if !exists {
			missing = append(missing, field)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Missing required parameter(s) for %s: %v\n", def.Name, missing))
	for _, field := range missing {
		if prop, ok := props[field].(map[string]any); ok {
			typ, _ := prop["type"].(string)
			desc, _ := prop["description"].(string)
			b.WriteString(fmt.Sprintf("- %s (%s): %s\n", field, typ, desc))
		} else {
			b.WriteString(fmt.Sprintf("- %s\n", field))
		}
	}
	b.WriteString("Please provide all required parameters.")
	return &Result{Content: b.String(), IsError: true}
}

// getRequiredFields extracts the "required" array from a JSON Schema object.
func getRequiredFields(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var fields []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				fields = append(fields, s)
			}
		}
		return fields
	}
	return nil
}
