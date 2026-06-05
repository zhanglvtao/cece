// Package runtime assembles the cece engine + mediator + tool registry
// + prompt assembler from a small set of options. The factory is shared
// by cmd/cece/main.go (production) and internal/testkit (E2E tests),
// so both paths exercise the exact same wiring.
package runtime

import (
	"context"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/lint"
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
	Engine    *engine.Engine
	Mediator  *engine.EngineMediator
	Store     session.Store
	Skills    *skill.Store
	PlanState *tool.PlanModeState
	TaskList  *tool.TaskList
	Registry  *tool.Registry
	Assembler *prompt.ContextAssembler

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
	ProjectDir    string
	Model         string
	ContextWindow int           // 0 → defaults to 200000
	MaxTokens     int           // 0 → 16384
	Yolo          bool          // auto-approve tool execution
	DefaultMode   string        // "" / "default" / "auto-accept" / "plan"
	StablePrompt  string        // "" → prompt.FormatStableSystemPrompt(ProjectDir)
	LintConfig    map[string]string

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
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(opts.ProjectDir)
	taskList := tool.NewTaskList()

	skillStore := opts.Skills
	if skillStore == nil {
		skillStore = skill.NewStore(nil)
	}

	registry := tool.NewRegistry(
		tool.NewBash(),
		tool.NewRead(),
		tool.NewWrite(),
		tool.NewGrep(),
		tool.NewEdit(),
		tool.NewGlob(),
		tool.NewWebFetch(),
		tool.NewEnterPlanMode(planState),
		tool.NewExitPlanMode(planState),
		tool.NewAskUserQuestion(),
		tool.NewSkillTool(skillStore),
		tool.NewTodo(taskList),
	)
	for _, t := range opts.ExtraTools {
		registry.Register(t)
	}

	if len(opts.LintConfig) > 0 {
		registry.SetLinter(lint.NewRunner(opts.LintConfig, opts.ProjectDir))
	}

	cleanup := func() { logger.Sync() }
	if opts.MCPManager != nil {
		mgr := opts.MCPManager
		cleanup = func() {
			mgr.Close()
			logger.Sync()
		}
		for _, t := range mgr.Tools() {
			registry.Register(t)
		}
	}

	stable := opts.StablePrompt
	if stable == "" {
		stable = prompt.FormatStableSystemPrompt(opts.ProjectDir)
	}
	collector := prompt.NewDefaultSessionCollector(opts.ProjectDir, registry)
	collector.SetSkillProvider(skillStore)
	assembler := prompt.NewContextAssembler(stable, registry, collector)

	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		logger.Warn("initial session refresh failed", "error", err)
	}

	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 200000
	}
	assembler.SetMaxContextTokens(contextWindow)

	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16384
	}

	eng := engine.NewEngine(opts.ModelClient, registry, opts.Yolo, maxTokens, assembler, opts.ProjectDir)
	eng.SetPlanModeState(planState)
	eng.SetTaskList(taskList)
	eng.SetModelInfo(opts.Model, contextWindow)
	registry.Register(tool.NewCompact(eng.CompactHandler()))
	registry.Register(tool.NewTrimToolResults(eng.TrimToolResultsHandler()))
	registry.Register(tool.NewPrune(eng.PruneHandler()))
	registry.Register(tool.NewAgent(eng.AgentHandler()))

	if opts.ContextWindowFor != nil {
		eng.ContextWindowFor = opts.ContextWindowFor
	}
	if opts.ModelClientFor != nil {
		eng.ModelClientFor = opts.ModelClientFor
	}
	if opts.Store != nil {
		eng.SetStore(opts.Store)
	}
	if opts.DefaultMode != "" {
		eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionMode(opts.DefaultMode)})
	}

	createFn := opts.CreateClientFn
	if createFn == nil {
		createFn = func(string, string, string, string, string, string, string) agent.ModelClient {
			return opts.ModelClient
		}
	}
	resolveFn := opts.ProviderResolver
	if resolveFn == nil {
		resolveFn = func(string) (string, string, string, string, string) {
			return "", "", "", "", ""
		}
	}
	listFn := opts.ListAllModelsFn
	if listFn == nil {
		listFn = func(context.Context) ([]protocol.ModelInfo, error) {
			return []protocol.ModelInfo{{ID: opts.Model, MaxContextWindow: contextWindow}}, nil
		}
	}

	mediator := engine.NewEngineMediator(eng, opts.Store, resolveFn, createFn, listFn, opts.MCPManager, opts.LightClientFn)

	return &Bundle{
		Engine:    eng,
		Mediator:  mediator,
		Store:     opts.Store,
		Skills:    skillStore,
		PlanState: planState,
		TaskList:  taskList,
		Registry:  registry,
		Assembler: assembler,
		Cleanup:   cleanup,
	}, nil
}
