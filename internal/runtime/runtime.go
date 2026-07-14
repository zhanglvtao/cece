// Package runtime assembles the cece engine + mediator + tool registry
// + prompt assembler from a small set of options. The factory is shared
// by cmd/cece/main.go (production) and internal/testkit (E2E tests),
// so both paths exercise the exact same wiring.
package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/mcp"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/tool"
)

// Bundle is the wired runtime returned by Build.
type Bundle struct {
	Engine      *engine.Engine
	Mediator    *engine.EngineMediator
	Store       session.Store
	Skills      *skill.Store
	PlanState   *tool.PlanModeState
	TaskList    *tool.TaskList
	TaskClosure *tool.TaskClosureState
	Registry    *tool.Registry
	Assembler   *prompt.ContextAssembler

	// Cleanup must be called on shutdown to release MCP / log resources.
	Cleanup func()
}

// ProviderResolverFn maps a provider config name to its credentials.
type ProviderResolverFn = func(configName string) (apiKey, baseURL, authMode, authHelper, protocol string)

// CreateClientFn constructs a model client for a (protocol, model, ...) tuple.
type CreateClientFn = func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient

// ListAllModelsFn lists models across all providers.
type ListAllModelsFn = func(ctx context.Context) ([]protocol.ModelInfo, error)

// ContextWindowFn maps a model ID to its context window (tokens).
type ContextWindowFn = func(model string) int

// LightModelClientFn returns a lightweight client for auxiliary tasks
// like title generation. nil → fall back to the primary client.
type LightModelClientFn = func() agent.ModelClient

// Options drives Build. Required fields: ProjectDir, Model, ModelClient.
// All other fields are optional with sensible zero-value defaults.
type Options struct {
	ProjectDir             string
	Model                  string
	ContextWindow          int    // 0 → defaults to 200000
	MaxTokens              int    // 0 → 16384
	Yolo                   bool   // auto-approve tool execution
	DefaultMode            string // "" / "default" / "auto-accept" / "plan"
	DefaultEffort          string // "low" / "medium" / "high" / "xhigh" / "auto"
	StablePrompt           string // "" → prompt.FormatStableSystemPrompt(ProjectDir)
	LintConfig             map[string]string
	PlanModeWriteAllowlist []string
	AgentModelChoices      []string

	ModelClient agent.ModelClient // required
	LightClient agent.ModelClient // optional

	Store      session.Store // optional; nil → no persistence
	Skills     *skill.Store  // optional; nil → empty
	ExtraTools []tool.Tool   // appended to registry
	MCPManager *mcp.Manager  // optional

	ProviderResolver ProviderResolverFn
	CreateClientFn   CreateClientFn
	ListAllModelsFn  ListAllModelsFn
	ContextWindowFor ContextWindowFn // used by Engine.ContextWindowFor
	ModelClientFor   func(model string) agent.ModelClient
	LightClientFn    LightModelClientFn
}

// Build wires every cece subsystem from opts. The returned Bundle
// owns no goroutines; Mediator.Wait() is the only blocking shutdown.
func Build(opts Options) (*Bundle, error) {
	skillStore := opts.Skills
	if skillStore == nil {
		skillStore = skill.NewStore(nil)
	}

	cleanup := func() { logger.Sync() }
	if opts.MCPManager != nil {
		mgr := opts.MCPManager
		cleanup = func() {
			mgr.Close()
			logger.Sync()
		}
	}
	builder := NewBuilder(SharedDeps{
		ProjectDir:             opts.ProjectDir,
		Store:                  opts.Store,
		Skills:                 skillStore,
		ExtraTools:             opts.ExtraTools,
		MCPManager:             opts.MCPManager,
		ProviderResolver:       opts.ProviderResolver,
		CreateClient:           opts.CreateClientFn,
		ListAllModels:          opts.ListAllModelsFn,
		ContextWindowFor:       opts.ContextWindowFor,
		ModelClientFor:         opts.ModelClientFor,
		LightClientFn:          opts.LightClientFn,
		MaxTokens:              opts.MaxTokens,
		LintConfig:             opts.LintConfig,
		PlanModeWriteAllowlist: opts.PlanModeWriteAllowlist,
		AgentModelChoices:      opts.AgentModelChoices,
	})

	built, err := builder.Build(context.Background(), BuildRequest{
		ID:            "interactive-root",
		Model:         opts.Model,
		ContextWindow: opts.ContextWindow,
		ModelClient:   opts.ModelClient,
		Profile:       MustProfile(ProfileInteractive),
		Yolo:          opts.Yolo,
		DefaultMode:   opts.DefaultMode,
		DefaultEffort: opts.DefaultEffort,
		StablePrompt:  opts.StablePrompt,
	})
	if err != nil {
		return nil, err
	}

	skillStore.OnChange = func() {
		if _, err := built.Assembler.RefreshSession(context.Background()); err != nil {
			logger.Warn("skill change refresh failed", "error", err)
		}
	}

	built.Engine.SetAgentController(engine.NewOrchestrator(&subAgentFactory{
		builder:          builder,
		projectDir:       opts.ProjectDir,
		parentEng:        built.Engine,
		defaultModel:     opts.Model,
		modelClientFor:   opts.ModelClientFor,
		contextWindowFor: opts.ContextWindowFor,
	}, opts.Store, built.Engine.EmitEvent))

	return &Bundle{
		Engine:      built.Engine,
		Mediator:    built.Mediator,
		Store:       opts.Store,
		Skills:      skillStore,
		PlanState:   built.PlanState,
		TaskList:    built.TaskList,
		TaskClosure: built.TaskClosure,
		Registry:    built.Registry,
		Assembler:   built.Assembler,
		Cleanup:     cleanup,
	}, nil
}

// subAgentFactory implements engine.SubAgentRuntimeFactory.
// It builds a full AgentRuntime with its own Engine + EngineMediator.
type subAgentFactory struct {
	builder          *Builder
	projectDir       string
	parentEng        *engine.Engine
	defaultModel     string
	modelClientFor   func(model string) agent.ModelClient
	contextWindowFor ContextWindowFn
}

func profileForAgentType(agentType string) (AgentProfile, error) {
	switch strings.TrimSpace(agentType) {
	case string(ProfileExplore):
		return MustProfile(ProfileExplore), nil
	case string(ProfileCoding):
		return MustProfile(ProfileCoding), nil
	case string(ProfileReview):
		return MustProfile(ProfileReview), nil
	case string(ProfileExecution):
		return MustProfile(ProfileExecution), nil
	default:
		return AgentProfile{}, fmt.Errorf("unknown agent_type: %s", agentType)
	}
}

func (f *subAgentFactory) NewSubAgentRuntime(ctx context.Context, cfg engine.SubAgentBuildConfig) (*engine.AgentRuntime, error) {
	subModel := strings.TrimSpace(cfg.Model)
	if subModel == "" && f.parentEng != nil {
		subModel = strings.TrimSpace(f.parentEng.SessionMetaModel())
	}
	if subModel == "" {
		subModel = strings.TrimSpace(f.defaultModel)
	}
	if subModel == "" {
		return nil, fmt.Errorf("sub-agent model is empty and no default model is configured")
	}
	contextWindow := 200000
	if f.contextWindowFor != nil {
		if cw := f.contextWindowFor(subModel); cw > 0 {
			contextWindow = cw
		}
	}
	var client agent.ModelClient
	if f.modelClientFor != nil {
		client = f.modelClientFor(subModel)
	}
	if client == nil {
		client = f.parentEng.Client()
	}
	profile, err := profileForAgentType(cfg.Profile)
	if err != nil {
		return nil, err
	}
	built, err := f.builder.Build(ctx, BuildRequest{
		ID:                cfg.AgentID,
		Description:       cfg.Description,
		Model:             subModel,
		ContextWindow:     contextWindow,
		ModelClient:       client,
		Profile:           profile,
		ParentSessionID:   cfg.ParentSessionID,
		SystemPromptExtra: cfg.SystemPromptExtra,
		ToolNames:         cfg.Tools,
		MaxTurns:          cfg.MaxTurns,
	})
	if err != nil {
		return nil, err
	}
	return built.Tracker, nil
}
