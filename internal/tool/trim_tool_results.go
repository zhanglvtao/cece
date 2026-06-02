package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const TrimToolResultsToolName = "TrimToolResults"

// TrimToolResultsHandler is the callback interface for the TrimToolResults tool.
type TrimToolResultsHandler struct {
	// TrimToolResults trims tool result content in turns [fromTurn, toTurn).
	// Returns (truncatedCount, tokensBefore, tokensAfter).
	TrimToolResults func(fromTurn, toTurn int) (int, int, int)
}

// NewTrimToolResults creates a TrimToolResults tool that replaces tool output
// content with '[trimmed]' in a specified turn range. Zero API cost.
func NewTrimToolResults(h *TrimToolResultsHandler) Tool {
	return trimToolResultsTool{handler: h}
}

type trimToolResultsTool struct {
	handler *TrimToolResultsHandler
}

func (trimToolResultsTool) Effect() Effect { return EffectMode }

func (trimToolResultsTool) Info() Definition {
	return Definition{
		Name:        TrimToolResultsToolName,
		Description: "Remove tool output content in a range of turns by replacing it with '[trimmed]'. Zero API cost. Use when older tool results are no longer needed verbatim but you want to keep the conversation structure intact.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"from_turn", "to_turn"},
			"properties": map[string]any{
				"from_turn": map[string]any{
					"type":        "integer",
					"description": "Start of turn range (inclusive, 0-based). Defaults to 0 (earliest turn) if not specified. Tool results in turns [from_turn, to_turn) will be trimmed.",
				},
				"to_turn": map[string]any{
					"type":        "integer",
					"description": "End of turn range (exclusive). Tool results in turns [from_turn, to_turn) will be trimmed.",
				},
			},
		},
	}
}

type trimToolResultsParams struct {
	FromTurn int `json:"from_turn"`
	ToTurn   int `json:"to_turn"`
}

func (t trimToolResultsTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil {
		return Result{Content: "trim tool results handler is not configured", IsError: true}
	}

	var p trimToolResultsParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	emitter.Emit("Trimming tool results...\n")

	fromTurn := p.FromTurn
	if fromTurn < 0 {
		fromTurn = 0
	}
	toTurn := p.ToTurn
	if toTurn <= 0 {
		return Result{Content: "to_turn is required and must be > 0.", IsError: true}
	}

	truncatedCount, tokensBefore, tokensAfter := t.handler.TrimToolResults(fromTurn, toTurn)

	slog.Info("trim_tool_results completed", "from_turn", fromTurn, "to_turn", toTurn, "truncated", truncatedCount, "tokens_before", tokensBefore, "tokens_after", tokensAfter)

	return Result{Content: fmt.Sprintf("Trimmed %d tool results in turns %d–%d. Estimated tokens: %dK → %dK.", truncatedCount, fromTurn, toTurn-1, (tokensBefore+999)/1000, (tokensAfter+999)/1000)}
}
