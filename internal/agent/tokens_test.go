package agent

import (
	"encoding/json"
	"testing"

	"cece/internal/prompt"
	"cece/internal/tool"
)

func TestEstimateRequestTokens_StructuralOverhead(t *testing.T) {
	// A minimal request should still account for structural overhead
	system := SystemPrompt{Blocks: []SystemBlock{{Text: "You are helpful."}}}
	messages := []Message{
		{Role: UserRole, Content: "Hello"},
		{Role: AssistantRole, Content: "Hi there!"},
	}
	tools := []tool.Definition{}

	estimated := EstimateRequestTokens(system, messages, tools)

	// Compute raw text tokens (no overhead) for comparison
	rawText := prompt.PreciseEstimateTokens("You are helpful.") +
		prompt.PreciseEstimateTokens("Hello") +
		prompt.PreciseEstimateTokens("Hi there!")
	overhead := estimated - rawText

	if overhead <= 0 {
		t.Errorf("structural overhead = %d, want > 0 (estimated=%d, rawText=%d)", overhead, estimated, rawText)
	}
}

func TestEstimateRequestTokens_ToolUseOverhead(t *testing.T) {
	// A message with a tool_use block should include tool_use structural overhead
	inputJSON := json.RawMessage(`{"command":"ls -la"}`)

	msgsWithTool := []Message{
		{Role: AssistantRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiTextContentType, Text: "I'll run that."},
			{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: inputJSON}},
		}},
		{Role: UserRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "call_1", Content: "file1.txt\nfile2.txt"}},
		}},
	}

	estimated := EstimateRequestTokens(SystemPrompt{}, msgsWithTool, nil)

	// Raw text: "I'll run that." + "Bash" + JSON + "file1.txt\nfile2.txt"
	rawText := prompt.PreciseEstimateTokens("I'll run that.") +
		prompt.PreciseEstimateTokens("Bash") +
		prompt.PreciseEstimateTokens(string(inputJSON)) +
		prompt.PreciseEstimateTokens("file1.txt\nfile2.txt")

	overhead := estimated - rawText
	// Should have: 2*overheadPerMessage + overheadTextBlock + overheadToolUseBlock + overheadToolResultBlock
	// = 2*5 + 3 + 8 + 6 = 27
	expectedOverhead := 2*overheadPerMessage + overheadTextBlock + overheadToolUseBlock + overheadToolResultBlock
	if overhead != expectedOverhead {
		t.Errorf("overhead = %d, want %d (estimated=%d, rawText=%d)", overhead, expectedOverhead, estimated, rawText)
	}
}

func TestEstimateRequestTokens_WithToolDefinitions(t *testing.T) {
	tools := []tool.Definition{
		{
			Name:        "Bash",
			Description: "Run a shell command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		},
	}

	estimated := EstimateRequestTokens(SystemPrompt{}, nil, tools)

	// Raw text: name + description + schema JSON
	schemaJSON, _ := json.Marshal(tools[0].InputSchema)
	rawText := prompt.PreciseEstimateTokens("Bash") +
		prompt.PreciseEstimateTokens("Run a shell command") +
		prompt.PreciseEstimateTokens(string(schemaJSON))

	overhead := estimated - rawText
	expectedOverhead := overheadPerToolDef
	if overhead != expectedOverhead {
		t.Errorf("overhead = %d, want %d", overhead, expectedOverhead)
	}
}

func TestEstimateRequestTokens_PreciseVsHeuristic(t *testing.T) {
	// Precise estimation should be consistently higher than heuristic for code-like content
	// because BPE tokenizes code more densely than the char-ratio heuristic assumes.
	codeContent := `func main() {
		fmt.Println("Hello, World!")
		for i := 0; i < 10; i++ {
			go func(n int) {
				time.Sleep(100 * time.Millisecond)
				fmt.Printf("goroutine %d\n", n)
			}(i)
		}
	}`

	heuristic := prompt.EstimateTokens(codeContent)
	precise := prompt.PreciseEstimateTokens(codeContent)

	// For code, precise should generally be >= heuristic
	// (heuristic underestimates because ~4 chars/token is too generous for code)
	t.Logf("code: heuristic=%d, precise=%d, ratio=%.2f", heuristic, precise, float64(precise)/float64(heuristic))
}

func TestEstimateMessagesTokens_ConsistentWithRequestTokens(t *testing.T) {
	messages := []Message{
		{Role: UserRole, Content: "Hello, can you help me?"},
		{Role: AssistantRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiTextContentType, Text: "Sure!"},
			{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{
				ID: "call_1", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`),
			}},
		}},
		{Role: UserRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{
				ToolUseID: "call_1", Content: "file1.go\nfile2.go",
			}},
		}},
	}

	// EstimateMessagesTokens should use the same precision as EstimateRequestTokens
	msgTokens := EstimateMessagesTokens(messages)

	// EstimateRequestTokens with empty system/tools should give same message count
	reqTokens := EstimateRequestTokens(SystemPrompt{}, messages, nil)

	// They should match (no system prompt overhead since Blocks is empty)
	if msgTokens != reqTokens {
		t.Errorf("EstimateMessagesTokens=%d != EstimateRequestTokens=%d", msgTokens, reqTokens)
	}
}
