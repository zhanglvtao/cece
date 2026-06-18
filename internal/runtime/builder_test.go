package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/tool"
)

type stubModelClient struct{}

func (stubModelClient) Stream(context.Context, []agent.Message, agent.SystemPrompt, []tool.Definition, int) (<-chan agent.ApiStreamEvent, error) {
	ch := make(chan agent.ApiStreamEvent)
	close(ch)
	return ch, nil
}

func (stubModelClient) SetReasoningEffort(string) {}

type memStore struct {
	mu        sync.Mutex
	sessions  map[string]*session.Session
	messages  map[string][]json.RawMessage
	idCounter atomic.Uint64
}

func newMemStore() *memStore {
	return &memStore{
		sessions: make(map[string]*session.Session),
		messages: make(map[string][]json.RawMessage),
	}
}

func (s *memStore) nextID() string {
	n := s.idCounter.Add(1)
	return fmt.Sprintf("test-session-%d", n)
}

func (s *memStore) Create(_ context.Context, title string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	sess := &session.Session{ID: s.nextID(), Title: title, CreatedAt: now, UpdatedAt: now}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *memStore) AppendMessage(_ context.Context, sessionID string, msg json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], append(json.RawMessage(nil), msg...))
	return nil
}

func (s *memStore) LoadMessages(_ context.Context, sessionID string) ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.messages[sessionID]
	out := make([]json.RawMessage, len(src))
	for i, m := range src {
		out[i] = append(json.RawMessage(nil), m...)
	}
	return out, nil
}

func (s *memStore) List(_ context.Context) ([]session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	return out, nil
}

func (s *memStore) Get(_ context.Context, id string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	cp := *sess
	return &cp, nil
}

func (s *memStore) Rename(_ context.Context, id, newTitle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.Title = newTitle
		sess.UpdatedAt = time.Now()
	}
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	delete(s.messages, id)
	return nil
}

func (s *memStore) UpdateMeta(_ context.Context, sessionID string, meta session.SessionMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Model = meta.Model
		sess.ContextWindow = meta.ContextWindow
		sess.Protocol = meta.Protocol
		sess.ConfigName = meta.ConfigName
		sess.LastInputTokens = meta.LastInputTokens
		sess.TotalInputTokens = meta.TotalInputTokens
		sess.TotalOutputTokens = meta.TotalOutputTokens
		sess.StatusBar = meta.StatusBar
		sess.UpdatedAt = time.Now()
	}
	return nil
}

func (s *memStore) SaveInputHistory(_ context.Context, sessionID string, history []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.InputHistory = append([]string(nil), history...)
		sess.UpdatedAt = time.Now()
	}
	return nil
}

var _ session.Store = (*memStore)(nil)

func TestBuilderBuildsInteractiveAndWorkerRuntimes(t *testing.T) {
	llm := stubModelClient{}
	builder := NewBuilder(SharedDeps{
		ProjectDir: t.TempDir(),
		Store:      newMemStore(),
		Skills:     skill.NewStore(nil),
		MaxTokens:  1024,
		CreateClient: func(string, string, string, string, string, string, string) agent.ModelClient {
			return llm
		},
		ModelClientFor: func(string) agent.ModelClient { return llm },
		ListAllModels: func(context.Context) ([]protocol.ModelInfo, error) {
			return []protocol.ModelInfo{{ID: "test-model", MaxContextWindow: 32000}}, nil
		},
	})

	interactive, err := builder.Build(context.Background(), BuildRequest{
		ID:            "interactive-root",
		Model:         "test-model",
		ContextWindow: 32000,
		ModelClient:   llm,
		Profile:       MustProfile(ProfileInteractive),
		Yolo:          true,
	})
	if err != nil {
		t.Fatalf("Build(interactive) error = %v", err)
	}
	if interactive.Engine == nil || interactive.Mediator == nil {
		t.Fatal("interactive runtime should include engine and mediator")
	}
	if interactive.Tracker != nil {
		t.Fatal("interactive runtime should not create worker tracker")
	}
	if _, ok := interactive.Registry.Get(tool.AgentToolName); !ok {
		t.Fatal("interactive registry should contain Agent tool")
	}

	worker, err := builder.Build(context.Background(), BuildRequest{
		ID:                "agent-1",
		Description:       "file analysis",
		Model:             "worker-model",
		ContextWindow:     16000,
		ModelClient:       llm,
		Profile:           MustProfile(ProfileWorker),
		ParentSessionID:   "sess-parent",
		SystemPromptExtra: "worker-only-instructions",
	})
	if err != nil {
		t.Fatalf("Build(worker) error = %v", err)
	}
	if worker.Tracker == nil {
		t.Fatal("worker runtime should create tracker")
	}
	if worker.Tracker.MaxTurns != MustProfile(ProfileWorker).Execution.DefaultMaxTurns {
		t.Fatalf("worker MaxTurns = %d, want %d", worker.Tracker.MaxTurns, MustProfile(ProfileWorker).Execution.DefaultMaxTurns)
	}
	if worker.Engine.Effort() != "low" {
		t.Fatalf("worker effort = %q, want low", worker.Engine.Effort())
	}
	if _, ok := worker.Registry.Get(tool.AgentToolName); ok {
		t.Fatal("worker registry must not contain Agent tool")
	}
	assembled := worker.Assembler.Assemble(prompt.TurnContext{})
	if !strings.Contains(assembled.FullText, "worker-only-instructions") {
		t.Fatalf("worker prompt missing extra instructions: %q", assembled.FullText)
	}
}

func TestBuildUsesBuilderForInteractiveBundle(t *testing.T) {
	llm := stubModelClient{}
	bundle, err := Build(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		DefaultEffort: "xhigh",
		ModelClient:   llm,
		Store:         newMemStore(),
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if bundle.Engine == nil || bundle.Mediator == nil {
		t.Fatal("bundle should expose engine and mediator")
	}
	if _, ok := bundle.Registry.Get(tool.AgentToolName); !ok {
		t.Fatal("interactive bundle should still contain Agent tool")
	}
	if bundle.Engine.Effort() != "xhigh" {
		t.Fatalf("Engine effort = %q, want xhigh", bundle.Engine.Effort())
	}
}

func TestBuilderLogsBuildStartAndComplete(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	orig := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(orig) })

	llm := stubModelClient{}
	_, err := Build(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   llm,
		Store:         newMemStore(),
	})
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}

	logs := buf.String()
	checks := []string{
		"runtime builder: build start",
		"runtime_id=interactive-root",
		"profile=interactive",
		"model=test-model",
		"runtime builder: build complete",
		"tool_count=",
		"agent_tool_enabled=true",
	}
	for _, check := range checks {
		if !strings.Contains(logs, check) {
			t.Fatalf("logs missing %q:\n%s", check, logs)
		}
	}
}
