package agent

import (
	"context"
	"encoding/json"
	"testing"

	"cece/internal/prompt"
	"cece/internal/session"
	"cece/internal/tool"
)

type dryRunEngine struct {
	assembler *prompt.ContextAssembler
	registry  *tool.Registry
	history   []Message
}

func (e *dryRunEngine) ProjectDir() string                      { return "/repo" }
func (e *dryRunEngine) Assembler() *prompt.ContextAssembler     { return e.assembler }
func (e *dryRunEngine) Client() ModelClient                     { return nil }
func (e *dryRunEngine) Registry() *tool.Registry                { return e.registry }
func (e *dryRunEngine) PlanState() *tool.PlanModeState          { return tool.NewPlanModeState() }
func (e *dryRunEngine) TaskList() *tool.TaskList                { return tool.NewTaskList() }
func (e *dryRunEngine) Yolo() bool                              { return false }
func (e *dryRunEngine) MaxTokens() int                          { return 1234 }
func (e *dryRunEngine) ToolResultPolicy() ToolResultPolicy      { return ToolResultPolicy{} }
func (e *dryRunEngine) SessionID() string                       { return "" }
func (e *dryRunEngine) HistoryLen() int                         { return len(e.history) }
func (e *dryRunEngine) AppendHistory(msg Message)               { e.history = append(e.history, msg) }
func (e *dryRunEngine) PersistMessage(context.Context, Message) {}
func (e *dryRunEngine) HistorySnapshot() []Message {
	return append([]Message(nil), e.history...)
}
func (e *dryRunEngine) SetLastInputTokens(int) {}
func (e *dryRunEngine) IncrementTokens(int, int) (string, session.SessionMeta, bool) {
	return "", session.SessionMeta{}, false
}
func (e *dryRunEngine) IncrementAPICalls()          {}
func (e *dryRunEngine) IncrementToolCount(string)   {}
func (e *dryRunEngine) UpdateCacheTokens(int, int)  {}
func (e *dryRunEngine) ResetQuestionAnswers()       {}
func (e *dryRunEngine) GetQuestionAnswers() []tool.QuestionAnswer { return nil }
func (e *dryRunEngine) DrainQueuedInputs() []string { return nil }
func (e *dryRunEngine) DrainNudgeReminder() string  { return "" }

type dryRunCollector struct{}

func (dryRunCollector) Collect(context.Context) (prompt.SessionContext, error) {
	return prompt.SessionContext{RepoRoot: "/repo", CLAUDEmd: "project rule"}, nil
}

type dryRunTool struct{}

func (dryRunTool) Info() tool.Definition {
	return tool.Definition{Name: "Read", Description: "read files", InputSchema: map[string]any{"type": "object"}}
}
func (dryRunTool) Run(context.Context, json.RawMessage, tool.Emitter) tool.Result {
	return tool.Result{}
}

func TestBuildDryRunRequestIncludesLayersMessagesAndTools(t *testing.T) {
	assembler := prompt.NewContextAssembler("stable prompt", nil, dryRunCollector{})
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	eng := &dryRunEngine{
		assembler: assembler,
		registry:  tool.NewRegistry(dryRunTool{}),
		history:   []Message{{Role: UserRole, Content: "old"}},
	}
	bootstrap := NewTurnBootstrap(eng, nil, nil, nil)
	plan := bootstrap.BuildTurnPlan("hello", []Message{{Role: UserRole, Content: "hello"}})
	dry := bootstrap.BuildDryRunRequest("hello", plan)

	if dry.Input != "hello" || dry.MaxTokens != 1234 {
		t.Fatalf("dryrun meta = %#v", dry)
	}
	if len(dry.PromptLayers) != 3 {
		t.Fatalf("layers = %d, want 3", len(dry.PromptLayers))
	}
	if dry.PromptLayers[0].Name != "stable" || dry.PromptLayers[1].Name != "session" || dry.PromptLayers[2].Name != "turn" {
		t.Fatalf("layers = %#v", dry.PromptLayers)
	}
	if len(dry.Messages) != 1 || dry.Messages[0].Content != "hello" {
		t.Fatalf("messages = %#v", dry.Messages)
	}
	if len(dry.Tools) != 1 || dry.Tools[0].Name != "Read" {
		t.Fatalf("tools = %#v", dry.Tools)
	}
	if dry.EstimatedInputTokens <= 0 {
		t.Fatalf("EstimatedInputTokens = %d, want > 0", dry.EstimatedInputTokens)
	}
}
