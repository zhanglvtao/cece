package openai

import "cece/internal/tool"

type OAITool struct {
	Type     string     `json:"type"`
	Function OAIToolDef `json:"function"`
}

type OAIToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ConvertTools converts Anthropic-format tool definitions to OpenAI function-calling format.
func ConvertTools(tools []tool.Definition) []OAITool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]OAITool, 0, len(tools))
	for _, t := range tools {
		result = append(result, OAITool{
			Type: "function",
			Function: OAIToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return result
}
