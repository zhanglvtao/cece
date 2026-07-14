package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/lint"
	"github.com/zhanglvtao/cece/internal/mcp"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/tool"
)

type SharedDeps struct {
	ProjectDir             string
	Store                  session.Store
	Skills                 *skill.Store
	ExtraTools             []tool.Tool
	MCPManager             *mcp.Manager
	ProviderResolver       ProviderResolverFn
	CreateClient           CreateClientFn
	ListAllModels          ListAllModelsFn
	ContextWindowFor       ContextWindowFn
	ModelClientFor         func(model string) agent.ModelClient
	LightClientFn          LightModelClientFn
	MaxTokens              int
	LintConfig             map[string]string
	PlanModeWriteAllowlist []string
	AgentModelChoices      []string
}

type BuildRequest struct {
	ID                string
	Description       string
	Model             string
	ContextWindow     int
	ModelClient       agent.ModelClient
	Profile           AgentProfile
	ParentSessionID   string
	SystemPromptExtra string
	ToolNames         []string
	Yolo              bool
	DefaultMode       string
	DefaultEffort     string
	StablePrompt      string
	MaxTurns          int
}

type BuiltRuntime struct {
	ID           string
	Profile      AgentProfile
	Engine       *engine.Engine
	Mediator     *engine.EngineMediator
	Registry     *tool.Registry
	Assembler    *prompt.ContextAssembler
	PlanState    *tool.PlanModeState
	TaskList     *tool.TaskList
	TaskClosure  *tool.TaskClosureState
	Tracker      *engine.AgentRuntime
	SessionStore session.Store
}

type Builder struct {
	shared SharedDeps
}

func NewBuilder(shared SharedDeps) *Builder {
	if shared.Skills == nil {
		shared.Skills = skill.NewStore(nil)
	}
	if shared.MaxTokens <= 0 {
		shared.MaxTokens = 16384
	}
	if shared.ProviderResolver == nil {
		shared.ProviderResolver = func(string) (string, string, string, string, string) {
			return "", "", "", "", ""
		}
	}
	if shared.CreateClient == nil {
		shared.CreateClient = func(string, string, string, string, string, string, string) agent.ModelClient {
			return nil
		}
	}
	return &Builder{shared: shared}
}

func (b *Builder) Build(ctx context.Context, req BuildRequest) (*BuiltRuntime, error) {
	if req.Profile.Name == "" {
		return nil, fmt.Errorf("profile is required")
	}
	if req.ID == "" {
		return nil, fmt.Errorf("runtime id is required")
	}

	client := req.ModelClient
	if client == nil && b.shared.ModelClientFor != nil && req.Model != "" {
		client = b.shared.ModelClientFor(req.Model)
	}
	if client == nil {
		return nil, fmt.Errorf("model client is required for profile %s", req.Profile.Name)
	}

	contextWindow := req.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 200000
		if b.shared.ContextWindowFor != nil && req.Model != "" {
			if cw := b.shared.ContextWindowFor(req.Model); cw > 0 {
				contextWindow = cw
			}
		}
	}
	listAllModels := b.shared.ListAllModels
	if listAllModels == nil {
		listAllModels = func(context.Context) ([]protocol.ModelInfo, error) {
			return []protocol.ModelInfo{{
				ID:               req.Model,
				MaxContextWindow: contextWindow,
			}}, nil
		}
	}

	planState := tool.NewPlanModeState()
	planState.SetProjectDir(b.shared.ProjectDir)
	planState.SetPlanModeWriteAllowPatterns(b.shared.PlanModeWriteAllowlist)
	taskList := tool.NewTaskList()
	taskClosure := tool.NewTaskClosureState()

	slog.Info("runtime builder: build start",
		"runtime_id", req.ID,
		"profile", req.Profile.Name,
		"model", req.Model,
	)

	registry := b.buildRegistry(req.Profile, req.ToolNames, planState, taskList, taskClosure)
	assembler := b.buildAssembler(ctx, req, registry, contextWindow)
	eng := engine.NewEngine(client, registry, req.Yolo, b.shared.MaxTokens, assembler, b.shared.ProjectDir)
	eng.SetPlanModeState(planState)
	eng.SetTaskList(taskList)
	eng.SetTaskClosureState(taskClosure)
	eng.SetStore(b.shared.Store)
	eng.SetModelInfo(req.Model, contextWindow)
	if req.DefaultMode != "" {
		eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionMode(req.DefaultMode)})
	}
	defaultEffort := req.DefaultEffort
	if defaultEffort == "" {
		defaultEffort = req.Profile.Execution.DefaultEffort
	}
	if defaultEffort != "" {
		eng.SetEffort(defaultEffort)
	}
	if b.shared.ContextWindowFor != nil {
		eng.ContextWindowFor = b.shared.ContextWindowFor
	}
	if b.shared.ModelClientFor != nil {
		eng.ModelClientFor = b.shared.ModelClientFor
	}

	registry.Register(tool.NewCompact(eng.CompactHandler()))
	registry.Register(tool.NewTrimToolResults(eng.TrimToolResultsHandler()))
	registry.Register(tool.NewPrune(eng.PruneHandler()))
	registry.Register(tool.NewSearchHistory(eng.SearchHistoryHandler()))
	if req.Profile.Tools.AllowAgentTool {
		registry.Register(tool.NewAgent(eng.AgentHandler(), tool.WithAgentModelProvider(func() []string {
			return append([]string{eng.SessionMetaModel()}, b.shared.AgentModelChoices...)
		})))
	}

	mediator := engine.NewEngineMediator(
		eng,
		b.shared.Store,
		b.shared.ProviderResolver,
		b.shared.CreateClient,
		listAllModels,
		b.shared.MCPManager,
		b.shared.LightClientFn,
	)

	built := &BuiltRuntime{
		ID:           req.ID,
		Profile:      req.Profile,
		Engine:       eng,
		Mediator:     mediator,
		Registry:     registry,
		Assembler:    assembler,
		PlanState:    planState,
		TaskList:     taskList,
		TaskClosure:  taskClosure,
		SessionStore: b.shared.Store,
	}

	if req.Profile.Name != ProfileInteractive {
		maxTurns := req.MaxTurns
		if maxTurns <= 0 {
			maxTurns = req.Profile.Execution.DefaultMaxTurns
		}
		rtCtx, cancel := context.WithCancel(ctx)
		built.Tracker = engine.NewAgentRuntime(
			req.ID,
			req.Description,
			req.Model,
			req.ParentSessionID,
			eng,
			mediator,
			rtCtx,
			cancel,
			maxTurns,
		)
	}

	slog.Info("runtime builder: build complete",
		"runtime_id", req.ID,
		"profile", req.Profile.Name,
		"model", req.Model,
		"tool_count", len(registry.Definitions()),
		"agent_tool_enabled", req.Profile.Tools.AllowAgentTool,
	)

	return built, nil
}

func (b *Builder) buildRegistry(profile AgentProfile, toolNames []string, planState *tool.PlanModeState, taskList *tool.TaskList, taskClosure *tool.TaskClosureState) *tool.Registry {
	readTracker := tool.NewReadTracker()
	registry := tool.NewRegistry(
		tool.NewBash(),
		tool.NewRead(readTracker),
		tool.NewWrite(readTracker),
		tool.NewGrep(),
		tool.NewEdit(readTracker),
		tool.NewGlob(),
		tool.NewWebFetch(),
		tool.NewEnterPlanMode(planState),
		tool.NewExitPlanMode(planState),
		tool.NewAskUserQuestion(),
		tool.NewSkillTool(b.shared.Skills),
		tool.NewTodo(taskList),
		tool.NewTaskClosure(taskClosure),
	)
	registry.SetResultStore(tool.NewResultStore(b.shared.ProjectDir))
	if len(b.shared.LintConfig) > 0 {
		registry.SetLinter(lint.NewRunner(b.shared.LintConfig, b.shared.ProjectDir))
	}
	if b.shared.MCPManager != nil {
		for _, t := range b.shared.MCPManager.Tools() {
			registry.Register(t)
		}
	}
	for _, t := range b.shared.ExtraTools {
		registry.Register(t)
	}

	excluded := map[string]struct{}{
		"Compact":                {},
		"TrimToolResults":        {},
		"Prune":                  {},
		"SearchHistory":          {},
		tool.TaskClosureToolName: {},
	}
	if !profile.Tools.AllowAgentTool {
		excluded[tool.AgentToolName] = struct{}{}
	}

	if len(toolNames) == 0 {
		return cloneRegistryWithout(registry, excluded, b.shared.LintConfig, b.shared.ProjectDir)
	}

	selected := tool.NewRegistry()
	selected.SetResultStore(registry.ResultStore())
	if len(b.shared.LintConfig) > 0 {
		selected.SetLinter(lint.NewRunner(b.shared.LintConfig, b.shared.ProjectDir))
	}
	for _, name := range toolNames {
		if _, skip := excluded[name]; skip {
			continue
		}
		if t, ok := registry.Get(name); ok {
			selected.Register(t)
		}
	}
	return selected
}

func (b *Builder) buildAssembler(ctx context.Context, req BuildRequest, registry *tool.Registry, contextWindow int) *prompt.ContextAssembler {
	stable := req.StablePrompt
	if stable == "" {
		stable = prompt.FormatStableSystemPrompt(b.shared.ProjectDir)
		if req.Profile.Name == ProfileInteractive {
			stable = prompt.FormatInteractiveSystemPrompt(b.shared.ProjectDir)
		}
		if req.Profile.Prompt.UseSubAgentPrompt {
			stable = prompt.FormatSubAgentSystemPrompt(b.shared.ProjectDir, string(req.Profile.Name), req.SystemPromptExtra)
		}
	}
	collector := prompt.NewDefaultSessionCollector(b.shared.ProjectDir, registry)
	collector.SetSkillProvider(b.shared.Skills)
	assembler := prompt.NewContextAssembler(stable, registry, collector)
	assembler.SetMaxContextTokens(contextWindow)
	if _, err := assembler.RefreshSession(ctx); err != nil {
		slog.Warn("runtime builder: initial session refresh failed", "error", err)
	}
	return assembler
}

func cloneRegistryWithout(src *tool.Registry, excluded map[string]struct{}, lintConfig map[string]string, projectDir string) *tool.Registry {
	out := tool.NewRegistry()
	out.SetResultStore(src.ResultStore())
	if len(lintConfig) > 0 {
		out.SetLinter(lint.NewRunner(lintConfig, projectDir))
	}
	for _, def := range src.Definitions() {
		if _, skip := excluded[def.Name]; skip {
			continue
		}
		if t, ok := src.Get(def.Name); ok {
			out.Register(t)
		}
	}
	return out
}