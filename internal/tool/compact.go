package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const CompactToolName = "Compact"

// compactHandler is the callback interface for the Compact tool.
type compactHandler struct {
	// Summary summarizes messages before the given turn and replaces history.
	// Returns (summary, tokensBefore, tokensAfter, error).
	Summary func(ctx context.Context, keepTurn int) (string, int, int, error)
}

// CompactHandler returns a compactHandler backed by engine callbacks.
type CompactHandler = compactHandler

// NewCompact creates a Compact tool that summarizes older conversation history.
func NewCompact(h *CompactHandler) Tool {
	return compactTool{handler: h}
}

type compactTool struct {
	handler *compactHandler
}

func (compactTool) Effect() Effect { return EffectMode }

func (compactTool) Info() Definition {
	return Definition{
		Name:        CompactToolName,
		Description: "Compress conversation context by generating an LLM summary of older turns and keeping recent turns verbatim. Costs API tokens but preserves semantic understanding. Use when the conversation is getting long, you've shifted to a new topic, or older context is still potentially relevant.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{},
			"properties": map[string]any{
				"turn": map[string]any{
					"type":        "integer",
					"description": "Turn number (0-based). Summarize turns before this one, keep this turn and later verbatim. Defaults to keeping the most recent 2 turns if not specified. Turn 0 is the earliest turn in the conversation.",
				},
			},
		},
	}
}

type compactParams struct {
	Turn int `json:"turn,omitempty"`
}

func (t compactTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil {
		return Result{Content: "compact handler is not configured", IsError: true}
	}

	var p compactParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	emitter.Emit("Summarizing conversation history...\n")

	keepTurn := p.Turn
	if keepTurn <= 0 {
		keepTurn = -1 // signal to handler: use default (totalTurns - 2)
	}

	summary, tokensBefore, tokensAfter, err := t.handler.Summary(ctx, keepTurn)
	if err != nil {
		return Result{Content: fmt.Sprintf("Failed to generate summary: %v", err), IsError: true}
	}

	slog.Info("compact summary completed", "tokens_before", tokensBefore, "tokens_after", tokensAfter)

	return Result{Content: fmt.Sprintf("Context compressed via summary. Estimated tokens: %dK → %dK.\n\nSummary:\n%s", (tokensBefore+999)/1000, (tokensAfter+999)/1000, summary)}
}
