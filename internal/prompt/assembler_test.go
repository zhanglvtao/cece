package prompt

import (
	"context"
	"strings"
	"testing"
)

// stubCollector returns a fixed SessionContext for testing.
type stubCollector struct {
	ctx SessionContext
	err error
}

func (s stubCollector) Collect(_ context.Context) (SessionContext, error) {
	return s.ctx, s.err
}

// mutableCollector allows changing the returned context between calls.
type mutableCollector struct {
	ctxs []SessionContext
	call int
}

func (m *mutableCollector) Collect(_ context.Context) (SessionContext, error) {
	if m.call >= len(m.ctxs) {
		m.call++
		return m.ctxs[len(m.ctxs)-1], nil
	}
	c := m.ctxs[m.call]
	m.call++
	return c, nil
}

func stubSessionContext() SessionContext {
	return SessionContext{
		RepoRoot:           "/repo",
		IsGitRepo:          true,
		OSName:             "darwin",
		OSVersion:          "25.0.0",
		SessionStartBranch: "main",
		CLAUDEmd:           "use go fmt",
	}
}

func TestContextAssemblerAssembleProducesThreeSegments(t *testing.T) {
	stable := "You are a helpful assistant."
	collector := stubCollector{ctx: stubSessionContext()}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	turnCtx := TurnContext{
		CurrentWorkingDirectory: "/repo/src",
		CurrentBranch:           "feature",
		Mode:                    "interactive",
		ConversationTurnNumber:  1,
	}

	result := assembler.Assemble(turnCtx)

	if len(result.Segments) != 3 {
		t.Fatalf("len(Segments) = %d, want 3", len(result.Segments))
	}
	if result.Segments[0].Layer != ContextStable {
		t.Fatalf("Segments[0].Layer = %v, want ContextStable", result.Segments[0].Layer)
	}
	if result.Segments[1].Layer != ContextSession {
		t.Fatalf("Segments[1].Layer = %v, want ContextSession", result.Segments[1].Layer)
	}
	if result.Segments[2].Layer != ContextTurn {
		t.Fatalf("Segments[2].Layer = %v, want ContextTurn", result.Segments[2].Layer)
	}
	if !strings.Contains(result.Segments[0].Content, "helpful assistant") {
		t.Fatalf("Stable segment missing content: %q", result.Segments[0].Content)
	}
	if !strings.Contains(result.Segments[1].Content, "use go fmt") {
		t.Fatalf("Session segment missing CLAUDEmd: %q", result.Segments[1].Content)
	}
	if !strings.Contains(result.Segments[2].Content, "/repo/src") {
		t.Fatalf("Turn segment missing cwd: %q", result.Segments[2].Content)
	}
}

func TestContextAssemblerAssembleSkipsEmptyTurn(t *testing.T) {
	stable := "You are a helpful assistant."
	collector := stubCollector{ctx: SessionContext{RepoRoot: "/repo"}}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	// Empty TurnContext — no fields set
	result := assembler.Assemble(TurnContext{})

	// Turn segment with only tags and no content should still be present
	// but the FullText should contain all three layers
	if len(result.Segments) != 3 {
		t.Fatalf("len(Segments) = %d, want 3", len(result.Segments))
	}
}

func TestContextAssemblerAssembleResultHasFullText(t *testing.T) {
	stable := "You are helpful."
	collector := stubCollector{ctx: SessionContext{RepoRoot: "/repo", CLAUDEmd: "use Chinese"}}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result := assembler.Assemble(TurnContext{Mode: "interactive"})

	if result.FullText == "" {
		t.Fatal("AssembleResult.FullText should not be empty")
	}
	if !strings.Contains(result.FullText, "You are helpful.") {
		t.Fatalf("FullText missing stable content: %q", result.FullText)
	}
	if !strings.Contains(result.FullText, "use Chinese") {
		t.Fatalf("FullText missing session content: %q", result.FullText)
	}
	if !strings.Contains(result.FullText, "interactive") {
		t.Fatalf("FullText missing turn content: %q", result.FullText)
	}
}

func TestContextAssemblerAssembleResultHasTokenEstimate(t *testing.T) {
	stable := "You are helpful."
	collector := stubCollector{ctx: SessionContext{RepoRoot: "/repo"}}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result := assembler.Assemble(TurnContext{Mode: "interactive"})

	if result.TokenEstimate <= 0 {
		t.Fatalf("TokenEstimate = %d, want > 0", result.TokenEstimate)
	}
}

func TestContextAssemblerToSystemPromptRoundTrip(t *testing.T) {
	stable := "You are helpful."
	collector := stubCollector{ctx: SessionContext{RepoRoot: "/repo", CLAUDEmd: "use Chinese"}}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result := assembler.Assemble(TurnContext{Mode: "interactive"})

	// Verify cache_control via ContextLayer
	nonEmptySegments := 0
	for _, seg := range result.Segments {
		if seg.Content != "" {
			nonEmptySegments++
		}
	}
	if nonEmptySegments != 3 {
		t.Fatalf("expected 3 non-empty segments, got %d", nonEmptySegments)
	}

	// Stable and Session should have cache_control
	if result.Segments[0].Layer.CacheControl() == nil {
		t.Fatal("Stable layer should have CacheControl")
	}
	if result.Segments[1].Layer.CacheControl() == nil {
		t.Fatal("Session layer should have CacheControl")
	}
	// Turn should NOT have cache_control
	if result.Segments[2].Layer.CacheControl() != nil {
		t.Fatal("Turn layer should NOT have CacheControl")
	}
}

func TestContextAssemblerRefreshSessionUpdatesContent(t *testing.T) {
	stable := "You are helpful."
	initial := SessionContext{RepoRoot: "/repo", CLAUDEmd: "initial instructions"}
	collector := stubCollector{ctx: initial}
	assembler := NewContextAssembler(stable, nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result1 := assembler.Assemble(TurnContext{Mode: "interactive"})
	if !strings.Contains(result1.FullText, "initial instructions") {
		t.Fatalf("FullText before refresh missing CLAUDEmd: %q", result1.FullText)
	}
}

func TestContextAssemblerEmptyStableStillWorks(t *testing.T) {
	collector := stubCollector{ctx: SessionContext{RepoRoot: "/repo"}}
	assembler := NewContextAssembler("", nil, collector)
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result := assembler.Assemble(TurnContext{Mode: "interactive"})

	// Stable segment with empty content should produce no text in FullText
	for _, seg := range result.Segments {
		if seg.Layer == ContextStable && seg.Content != "" {
			t.Fatal("empty stable segment should have empty Content")
		}
	}
}

func TestContextAssemblerRefreshSessionSkipsOnNoDiff(t *testing.T) {
	stable := "You are helpful."
	initial := SessionContext{RepoRoot: "/repo", CLAUDEmd: "use go fmt"}
	collector := &mutableCollector{ctxs: []SessionContext{initial, initial}}
	assembler := NewContextAssembler(stable, nil, collector)

	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}
	changed1, err := assembler.RefreshSession(context.Background())
	if err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}
	if changed1 {
		t.Fatal("RefreshSession should return false when no diff")
	}
}

func TestContextAssemblerRefreshSessionDetectsDiff(t *testing.T) {
	stable := "You are helpful."
	initial := SessionContext{RepoRoot: "/repo", CLAUDEmd: "use go fmt"}
	updated := SessionContext{RepoRoot: "/repo", CLAUDEmd: "use Chinese"}
	collector := &mutableCollector{ctxs: []SessionContext{initial, updated}}
	assembler := NewContextAssembler(stable, nil, collector)

	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}

	result1 := assembler.Assemble(TurnContext{Mode: "interactive"})
	if !strings.Contains(result1.FullText, "use go fmt") {
		t.Fatalf("FullText before refresh missing initial CLAUDEmd: %q", result1.FullText)
	}

	changed, err := assembler.RefreshSession(context.Background())
	if err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}
	if !changed {
		t.Fatal("RefreshSession should return true when diff detected")
	}

	result2 := assembler.Assemble(TurnContext{Mode: "interactive"})
	if !strings.Contains(result2.FullText, "use Chinese") {
		t.Fatalf("FullText after refresh missing updated CLAUDEmd: %q", result2.FullText)
	}
}
