package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const PruneToolName = "Prune"

// PruneHandler is the callback interface for the Prune tool.
type PruneHandler struct {
	// Prune deletes all messages before the given turn and replaces history.
	// Returns (tokensBefore, tokensAfter).
	Prune func(turn int) (int, int)
}

// NewPrune creates a Prune tool that deletes old messages before a given turn.
// Zero API cost, most aggressive context reduction.
func NewPrune(h *PruneHandler) Tool {
	return pruneTool{handler: h}
}

type pruneTool struct {
	handler *PruneHandler
}

func (pruneTool) Effect() Effect { return EffectMode }

func (pruneTool) Info() Definition {
	return Definition{
		Name:        PruneToolName,
		Description: "Delete all messages before a given turn entirely. Zero API cost, most aggressive context reduction. Use when older context is no longer needed and you want maximum context savings.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"turn"},
			"properties": map[string]any{
				"turn": map[string]any{
					"type":        "integer",
					"description": "Turn number (0-based). Delete all messages before this turn. Must be >= 1 and may equal the total turn count to prune through the current end. Turn 0 is the earliest turn in the conversation.",
				},
			},
		},
	}
}

type pruneParams struct {
	Turn int `json:"turn"`
}

func (t pruneTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil {
		return Result{Content: "prune handler is not configured", IsError: true}
	}

	var p pruneParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	emitter.Emit("Pruning old messages...\n")

	turn := p.Turn
	if turn < 1 {
		return Result{Content: "turn parameter is required and must be >= 1.", IsError: true}
	}

	tokensBefore, tokensAfter := t.handler.Prune(turn)

	slog.Info("prune completed", "pruned_turns", turn, "tokens_before", tokensBefore, "tokens_after", tokensAfter)

	return Result{Content: fmt.Sprintf("Pruned %d turns of conversation history. Kept turns %d onward. Estimated tokens: %dK → %dK.", turn, turn, (tokensBefore+999)/1000, (tokensAfter+999)/1000)}
}
