package prompt

// tokenBudget holds per-layer token limits derived from the model's context window.
type tokenBudget struct {
	total   int // total system prompt token budget
	stable  int // max tokens for stable context
	session int // max tokens for session context
	turn    int // max tokens for turn context
}

// budgetThreshold is the ratio at which enforceBudget switches from
// heuristic estimation to tiktoken precise counting.
const budgetThreshold = 0.7

// allocationRatios define how the total system prompt budget is split.
// The system prompt budget itself is ~25% of the full context window,
// leaving 75% for messages and tool definitions.
const (
	systemPromptRatio = 0.25
	stableRatio       = 0.15 // of system prompt budget
	sessionRatio      = 0.55 // of system prompt budget
	turnRatio         = 0.10 // of system prompt budget
	// remaining ~20% is reserve
)

func newTokenBudget(contextWindow int) tokenBudget {
	systemBudget := int(float64(contextWindow) * systemPromptRatio)
	return tokenBudget{
		total:   systemBudget,
		stable:  int(float64(systemBudget) * stableRatio),
		session: int(float64(systemBudget) * sessionRatio),
		turn:    int(float64(systemBudget) * turnRatio),
	}
}

// enforceBudget truncates segments to fit within the token budget.
// Truncation priority: Turn (drop first) > Session (truncate tools) > Stable (never).
// Uses two-phase estimation: heuristic below 70% threshold, tiktoken above.
func enforceBudget(segments []PromptSegment, maxContextTokens int) []PromptSegment {
	budget := newTokenBudget(maxContextTokens)
	result := make([]PromptSegment, len(segments))
	copy(result, segments)

	// Phase 1: heuristic total estimate
	var heuristicTotal int
	for _, seg := range result {
		heuristicTotal += estimateTokens(seg.Content)
	}

	threshold := int(float64(budget.total) * budgetThreshold)
	if heuristicTotal < threshold {
		return result // well under budget, no action needed
	}

	// Phase 2: precise estimate since we're near the budget
	var preciseTotal int
	preciseCosts := make([]int, len(result))
	for i, seg := range result {
		preciseCosts[i] = preciseEstimate(seg.Content)
		preciseTotal += preciseCosts[i]
	}

	if preciseTotal <= budget.total {
		return result // precise count says we're fine
	}

	// Truncation: Turn first
	for i, seg := range result {
		if seg.Layer == ContextTurn && preciseTotal > budget.total {
			saved := preciseCosts[i]
			result[i] = PromptSegment{Content: "", Layer: ContextTurn}
			preciseTotal -= saved
		}
	}

	if preciseTotal <= budget.total {
		return result
	}

	return result
}
