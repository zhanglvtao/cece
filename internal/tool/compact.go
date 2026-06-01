package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const CompactToolName = "Compact"

// CompactHandler is the callback interface for Compact tool operations.
// The engine implements these methods to perform the actual history manipulation.
// Using function fields avoids circular imports between tool and agent packages.
type CompactHandler struct {
	// Summary summarizes messages before the given turn and replaces history.
	// Returns (summary, tokensBefore, tokensAfter, error).
	Summary func(ctx context.Context, keepTurn int) (string, int, int, error)

	// TrimToolResults trims tool result content in turns [fromTurn, toTurn).
	// Returns (truncatedCount, tokensBefore, tokensAfter).
	TrimToolResults func(fromTurn, toTurn int) (int, int, int)

	// Prune deletes all messages before the given turn and replaces history.
	// Returns (tokensBefore, tokensAfter).
	Prune func(turn int) (int, int)
}

type compactTool struct {
	handler *CompactHandler
}

// NewCompact creates a Compact tool with the given handler.
func NewCompact(handler *CompactHandler) Tool {
	return compactTool{handler: handler}
}

func (compactTool) Effect() Effect { return EffectMode }

func (compactTool) Info() Definition {
	return Definition{
		Name:        CompactToolName,
		Description: "Compress conversation context. Use this tool proactively — you are responsible for managing your own context window. Compact when: the conversation is getting long, you've shifted to a new topic, older context is no longer needed, or you feel your attention is being diluted. Choose the strategy that fits: 'summary' for LLM-generated summaries (costs API tokens), 'trim_tool_results' to remove tool output content (free), or 'prune' to delete old messages entirely (free, most aggressive).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"strategy"},
			"properties": map[string]any{
				"strategy": map[string]any{
					"type":        "string",
					"enum":        []string{"summary", "trim_tool_results", "prune"},
					"description": "Compression strategy: 'summary' generates an LLM summary of older turns and keeps recent turns verbatim; 'trim_tool_results' replaces tool result content with '[trimmed]' in a turn range (zero API cost); 'prune' deletes all messages before a turn entirely (zero API cost, most aggressive).",
				},
				"turn": map[string]any{
					"type":        "integer",
					"description": "Turn number (0-based). For 'summary': summarize turns before this one, keep this turn and later verbatim. For 'prune': delete all messages before this turn. Turn 0 is the earliest turn in the conversation.",
				},
				"from_turn": map[string]any{
					"type":        "integer",
					"description": "Start of turn range for 'trim_tool_results' (inclusive, 0-based). Defaults to 0 (earliest turn) if not specified. Tool results in turns [from_turn, to_turn) will be trimmed.",
				},
				"to_turn": map[string]any{
					"type":        "integer",
					"description": "End of turn range for 'trim_tool_results' (exclusive). Tool results in turns [from_turn, to_turn) will be trimmed. Required when strategy is 'trim_tool_results'.",
				},
			},
		},
	}
}

type compactParams struct {
	Strategy string `json:"strategy"`
	Turn     int    `json:"turn,omitempty"`
	FromTurn int    `json:"from_turn,omitempty"`
	ToTurn   int    `json:"to_turn,omitempty"`
}

func (t compactTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil {
		return Result{Content: "compact handler is not configured", IsError: true}
	}

	var p compactParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	switch p.Strategy {
	case "summary":
		return t.runSummary(ctx, p, emitter)
	case "trim_tool_results":
		return t.runTrimToolResults(p, emitter)
	case "prune":
		return t.runPrune(p, emitter)
	default:
		return Result{Content: fmt.Sprintf("unknown strategy: %s (must be summary, trim_tool_results, or prune)", p.Strategy), IsError: true}
	}
}

func (t compactTool) runSummary(ctx context.Context, p compactParams, emitter Emitter) Result {
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

func (t compactTool) runTrimToolResults(p compactParams, emitter Emitter) Result {
	emitter.Emit("Trimming tool results...\n")

	fromTurn := p.FromTurn
	if fromTurn < 0 {
		fromTurn = 0
	}
	toTurn := p.ToTurn
	if toTurn <= 0 {
		return Result{Content: "to_turn is required for trim_tool_results strategy.", IsError: true}
	}

	truncatedCount, tokensBefore, tokensAfter := t.handler.TrimToolResults(fromTurn, toTurn)

	slog.Info("compact trim_tool_results completed", "from_turn", fromTurn, "to_turn", toTurn, "truncated", truncatedCount, "tokens_before", tokensBefore, "tokens_after", tokensAfter)

	return Result{Content: fmt.Sprintf("Trimmed %d tool results in turns %d–%d. Estimated tokens: %dK → %dK.", truncatedCount, fromTurn, toTurn-1, (tokensBefore+999)/1000, (tokensAfter+999)/1000)}
}

func (t compactTool) runPrune(p compactParams, emitter Emitter) Result {
	emitter.Emit("Pruning old messages...\n")

	turn := p.Turn
	if turn <= 0 {
		return Result{Content: "turn parameter is required for prune strategy (must be >= 1).", IsError: true}
	}

	tokensBefore, tokensAfter := t.handler.Prune(turn)

	slog.Info("compact prune completed", "pruned_turns", turn, "tokens_before", tokensBefore, "tokens_after", tokensAfter)

	return Result{Content: fmt.Sprintf("Pruned %d turns of conversation history. Kept turns %d onward. Estimated tokens: %dK → %dK.", turn, turn, (tokensBefore+999)/1000, (tokensAfter+999)/1000)}
}
