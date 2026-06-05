package aiden

import "github.com/zhanglvtao/cece/internal/tool"

type AidenTool struct {
	Type     string       `json:"type"`
	Function AidenToolDef `json:"function"`
}

type AidenToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ResponsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

func ConvertTools(tools []tool.Definition) []AidenTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]AidenTool, 0, len(tools))
	for _, t := range tools {
		result = append(result, AidenTool{
			Type: "function",
			Function: AidenToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return result
}

func ConvertResponsesTools(tools []tool.Definition) []ResponsesTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]ResponsesTool, 0, len(tools))
	for _, t := range tools {
		result = append(result, ResponsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}
	return result
}
