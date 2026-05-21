package prompt

import (
	"context"
	"strings"
	"sync"
)

// defaultContextWindow is the fallback context window size when
// the model info API is unavailable.
const defaultContextWindow = 200000

type ContextAssembler struct {
	mu               sync.RWMutex
	stable           string
	session          SessionContext
	sessionRendered  string
	collector        SessionCollector
	maxContextTokens int // model's max context window, used for budget enforcement
}

func NewContextAssembler(stablePrompt string, _ ToolListProvider, collector SessionCollector) *ContextAssembler {
	return &ContextAssembler{
		stable:           stablePrompt,
		collector:        collector,
		maxContextTokens: defaultContextWindow,
	}
}

// SetMaxContextTokens sets the model's context window size for budget enforcement.
// Should be called once after querying the model info API.
func (a *ContextAssembler) SetMaxContextTokens(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > 0 {
		a.maxContextTokens = n
	}
}

// RefreshSession re-collects and re-renders the session layer.
// Returns true if the rendered content changed (diff detected).
func (a *ContextAssembler) RefreshSession(ctx context.Context) (bool, error) {
	sc, err := a.collector.Collect(ctx)
	if err != nil {
		return false, err
	}
	rendered := FormatSessionContext(sc)

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.sessionRendered == rendered {
		return false, nil
	}
	a.session = sc
	a.sessionRendered = rendered
	return true, nil
}

// Assemble combines the three context layers into a single result.
func (a *ContextAssembler) Assemble(turnCtx TurnContext) AssembleResult {
	a.mu.RLock()
	sessionRendered := a.sessionRendered
	maxContextTokens := a.maxContextTokens
	a.mu.RUnlock()

	turnRendered := FormatTurnContext(turnCtx)

	segments := []PromptSegment{
		{Content: a.stable, Layer: ContextStable},
		{Content: sessionRendered, Layer: ContextSession},
		{Content: turnRendered, Layer: ContextTurn},
	}

	segments = enforceBudget(segments, maxContextTokens)

	var parts []string
	for _, seg := range segments {
		if seg.Content != "" {
			parts = append(parts, seg.Content)
		}
	}
	fullText := strings.Join(parts, "\n\n")

	return AssembleResult{
		Segments:      segments,
		FullText:      fullText,
		TokenEstimate: estimateTokens(fullText),
	}
}
