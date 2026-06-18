# Unify Runtime Builder and Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 cece 当前 interactive runtime 与 subagent 特例重构为统一的 `AgentRuntimeBuilder + AgentProfile + Orchestrator + RuntimeHost` 模型，同时保留现有 TUI / protocol 基本行为与 `Agent` tool 外部 schema。

**Architecture:** 保持 `internal/engine` 只负责单 Agent turn loop 与会话状态，把 worker 生命周期、pending state、异步控制操作抽到新的 `Orchestrator`。先在 `internal/runtime` 提取 profile 与 builder，再把 `Agent` tool 接到 orchestrator，最后用 `RuntimeHost` 替换当前 stdio 根对象；首版继续复用现有 `SubAgent*Event` 作为 TUI 兼容层。

**Tech Stack:** Go, `internal/runtime`, `internal/engine`, `internal/tool`, `internal/testkit`, Bubble Tea, `log/slog`, `go test`.

---

## File Structure

- Create: `internal/runtime/profile.go`
  - 定义 `ProfileName`、`AgentProfile`、默认 `interactive` / `worker` profile。
- Create: `internal/runtime/profile_test.go`
  - 锁定 profile 默认值与未知 profile 错误路径。
- Create: `internal/runtime/builder.go`
  - 提供统一的 `Builder`、`BuildRequest`、`BuiltRuntime`，把 interactive / worker 的 runtime 装配统一起来。
- Create: `internal/runtime/builder_test.go`
  - 覆盖 registry、prompt policy、mediator、tracker、default max turns 等 builder 行为。
- Create: `internal/engine/orchestrator.go`
  - 提供异步 worker control plane：`start/status/send/answer/confirm/reject/cancel/switch_model`，并维护 runtime registry / pending state / 兼容事件桥接。
- Create: `internal/engine/orchestrator_test.go`
  - 覆盖异步 start、状态查询、取消、控制面错误和日志 smoke。
- Create: `internal/tool/agent_tool_test.go`
  - 锁定 Agent tool 的异步文案与 running/status 返回值行为。
- Create: `internal/runtime/host.go`
  - 提供进程级 `RuntimeHost`，实现 `ipc.Runtime`，持有 foreground interactive runtime 与 orchestrator。
- Create: `internal/runtime/host_test.go`
  - 覆盖 host 的 `Input/Do/Events/Wait` 委托行为。
- Modify: `internal/runtime/runtime.go`
  - 让遗留 `runtime.Build(...)` 成为 builder 的兼容包装，并移除直接构造 subagent runtime 的重复逻辑。
- Modify: `internal/engine/engine.go`
  - 去掉 `subAgents` / `startSubAgent*` / `bridgeSubRuntimeEvents` 等多 Agent 逻辑，只保留 `AgentController` 注入点与单 Agent helpers。
- Modify: `internal/tool/agent_tool.go`
  - 更新工具描述与结果格式，让 `start` 明确是异步语义。
- Modify: `cmd/cece/main.go`
  - stdio engine 根对象从 `*engine.EngineMediator` 切到 `*runtime.Host`。
- Modify: `internal/testkit/harness.go`
  - test harness 改成通过 `RuntimeHost` 启动，但继续暴露 `Harness.Eng` / `Harness.Med` 供现有测试复用。
- Modify: `internal/testkit/e2e_advanced_test.go`
  - 把旧的同步 subagent 测试改成 async `start -> status -> complete` 流程。
- Modify: `internal/testkit/e2e_scenarios_test.go`
  - 添加 `status` / `cancel` / pending 相关的回归场景。

---

### Task 1: 定义 `AgentProfile` 与默认 profile

**Files:**
- Create: `internal/runtime/profile.go`
- Create: `internal/runtime/profile_test.go`

- [ ] **Step 1: 先写 profile 默认值测试**

在 `internal/runtime/profile_test.go` 新建下面的测试文件：

```go
package runtime

import "testing"

func TestProfileByName_Defaults(t *testing.T) {
	interactive, err := ProfileByName(ProfileInteractive)
	if err != nil {
		t.Fatalf("ProfileByName(interactive) error = %v", err)
	}
	if interactive.Name != ProfileInteractive {
		t.Fatalf("interactive.Name = %q", interactive.Name)
	}
	if !interactive.Tools.AllowAgentTool {
		t.Fatal("interactive profile should allow Agent tool")
	}
	if !interactive.Interaction.UserFacing {
		t.Fatal("interactive profile should be user-facing")
	}
	if !interactive.Spawn.AllowChildAgents {
		t.Fatal("interactive profile should allow spawning worker agents")
	}
	if interactive.Execution.DefaultMaxTurns != 0 {
		t.Fatalf("interactive DefaultMaxTurns = %d, want 0", interactive.Execution.DefaultMaxTurns)
	}

	worker, err := ProfileByName(ProfileWorker)
	if err != nil {
		t.Fatalf("ProfileByName(worker) error = %v", err)
	}
	if worker.Name != ProfileWorker {
		t.Fatalf("worker.Name = %q", worker.Name)
	}
	if worker.Tools.AllowAgentTool {
		t.Fatal("worker profile must not allow Agent tool")
	}
	if worker.Interaction.UserFacing {
		t.Fatal("worker profile must not be user-facing")
	}
	if !worker.Interaction.PendingToParent {
		t.Fatal("worker profile should route question/confirm/plan to parent")
	}
	if !worker.Result.ArtifactFirst {
		t.Fatal("worker profile should prefer artifact-first results")
	}
	if worker.Execution.DefaultEffort != "low" {
		t.Fatalf("worker DefaultEffort = %q, want low", worker.Execution.DefaultEffort)
	}
	if worker.Execution.DefaultMaxTurns != 8 {
		t.Fatalf("worker DefaultMaxTurns = %d, want 8", worker.Execution.DefaultMaxTurns)
	}
	if worker.Spawn.AllowChildAgents {
		t.Fatal("worker profile must not allow spawning child agents in v1")
	}
}

func TestProfileByName_UnknownProfile(t *testing.T) {
	if _, err := ProfileByName(ProfileName("reviewer")); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestMustProfile_PanicsOnUnknownProfile(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustProfile should panic for unknown profile")
		}
	}()
	_ = MustProfile(ProfileName("unknown"))
}
```

- [ ] **Step 2: 运行测试，先看到失败**

Run:

```bash
go test ./internal/runtime -run 'TestProfileByName|TestMustProfile' -count=1
```

Expected: FAIL，报 `undefined: ProfileByName`、`undefined: ProfileInteractive` 一类错误。

- [ ] **Step 3: 实现 profile 类型与默认值**

在 `internal/runtime/profile.go` 写入下面的实现：

```go
package runtime

import "fmt"

type ProfileName string

const (
	ProfileInteractive ProfileName = "interactive"
	ProfileWorker      ProfileName = "worker"
)

type PromptPolicy struct {
	UseSubAgentPrompt bool
}

type ToolPolicy struct {
	AllowAgentTool bool
}

type InteractionPolicy struct {
	UserFacing      bool
	PendingToParent bool
}

type ResultPolicy struct {
	ArtifactFirst   bool
	PreviewMaxChars int
}

type ExecutionPolicy struct {
	DefaultEffort   string
	DefaultMaxTurns int
}

type SpawnPolicy struct {
	AllowChildAgents bool
	AllowedProfiles  []ProfileName
}

type AgentProfile struct {
	Name        ProfileName
	Prompt      PromptPolicy
	Tools       ToolPolicy
	Interaction InteractionPolicy
	Result      ResultPolicy
	Execution   ExecutionPolicy
	Spawn       SpawnPolicy
}

func defaultProfiles() map[ProfileName]AgentProfile {
	return map[ProfileName]AgentProfile{
		ProfileInteractive: {
			Name: ProfileInteractive,
			Prompt: PromptPolicy{
				UseSubAgentPrompt: false,
			},
			Tools: ToolPolicy{
				AllowAgentTool: true,
			},
			Interaction: InteractionPolicy{
				UserFacing:      true,
				PendingToParent: false,
			},
			Result: ResultPolicy{
				ArtifactFirst:   false,
				PreviewMaxChars: 16000,
			},
			Execution: ExecutionPolicy{
				DefaultEffort:   "",
				DefaultMaxTurns: 0,
			},
			Spawn: SpawnPolicy{
				AllowChildAgents: true,
				AllowedProfiles:  []ProfileName{ProfileWorker},
			},
		},
		ProfileWorker: {
			Name: ProfileWorker,
			Prompt: PromptPolicy{
				UseSubAgentPrompt: true,
			},
			Tools: ToolPolicy{
				AllowAgentTool: false,
			},
			Interaction: InteractionPolicy{
				UserFacing:      false,
				PendingToParent: true,
			},
			Result: ResultPolicy{
				ArtifactFirst:   true,
				PreviewMaxChars: 16000,
			},
			Execution: ExecutionPolicy{
				DefaultEffort:   "low",
				DefaultMaxTurns: 8,
			},
			Spawn: SpawnPolicy{
				AllowChildAgents: false,
			},
		},
	}
}

func ProfileByName(name ProfileName) (AgentProfile, error) {
	profile, ok := defaultProfiles()[name]
	if !ok {
		return AgentProfile{}, fmt.Errorf("unknown agent profile: %s", name)
	}
	return profile, nil
}

func MustProfile(name ProfileName) AgentProfile {
	profile, err := ProfileByName(name)
	if err != nil {
		panic(err)
	}
	return profile
}
```

- [ ] **Step 4: 重新运行 profile 测试**

Run:

```bash
go test ./internal/runtime -run 'TestProfileByName|TestMustProfile' -count=1
```

Expected: PASS.

- [ ] **Step 5: 提交这一小步**

```bash
git add internal/runtime/profile.go internal/runtime/profile_test.go
git commit -m "refactor: define agent runtime profiles

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

### Task 2: 提取统一 `Builder`，让 interactive / worker 共用装配流程

**Files:**
- Create: `internal/runtime/builder.go`
- Create: `internal/runtime/builder_test.go`
- Modify: `internal/runtime/runtime.go`

- [ ] **Step 1: 先写 builder 的失败测试**

在 `internal/runtime/builder_test.go` 新建下面的测试：

```go
package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/testkit"
	"github.com/zhanglvtao/cece/internal/tool"
)

func TestBuilderBuildsInteractiveAndWorkerRuntimes(t *testing.T) {
	llm := testkit.NewScriptedClient()
	builder := NewBuilder(SharedDeps{
		ProjectDir:   t.TempDir(),
		Store:        testkit.NewMemStore(),
		Skills:       skill.NewStore(nil),
		MaxTokens:    1024,
		CreateClient: func(string, string, string, string, string, string, string) agent.ModelClient { return llm },
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
	if _, ok := worker.Registry.Get(tool.AgentToolName); ok {
		t.Fatal("worker registry must not contain Agent tool")
	}
	assembled := worker.Assembler.Assemble(prompt.TurnContext{})
	if !strings.Contains(assembled.FullText, "worker-only-instructions") {
		t.Fatalf("worker prompt missing extra instructions: %q", assembled.FullText)
	}
}

func TestBuildUsesBuilderForInteractiveBundle(t *testing.T) {
	llm := testkit.NewScriptedClient()
	bundle, err := Build(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   llm,
		Store:         testkit.NewMemStore(),
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
}
```

- [ ] **Step 2: 运行 builder 测试，确认失败**

Run:

```bash
go test ./internal/runtime -run 'TestBuilderBuildsInteractiveAndWorkerRuntimes|TestBuildUsesBuilderForInteractiveBundle' -count=1
```

Expected: FAIL，报 `undefined: NewBuilder`、`undefined: BuildRequest`、`undefined: SharedDeps` 一类错误。

- [ ] **Step 3: 新增统一 builder**

在 `internal/runtime/builder.go` 写入下面的核心实现：

```go
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
	ProjectDir       string
	Store            session.Store
	Skills           *skill.Store
	ExtraTools       []tool.Tool
	MCPManager       *mcp.Manager
	ProviderResolver ProviderResolverFn
	CreateClient     CreateClientFn
	ListAllModels    ListAllModelsFn
	ContextWindowFor ContextWindowFn
	ModelClientFor   func(model string) agent.ModelClient
	LightClientFn    LightModelClientFn
	MaxTokens        int
	LintConfig       map[string]string
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
	StablePrompt      string
	MaxTurns          int
}

type BuiltRuntime struct {
	ID          string
	Profile     AgentProfile
	Engine      *engine.Engine
	Mediator    *engine.EngineMediator
	Registry    *tool.Registry
	Assembler   *prompt.ContextAssembler
	PlanState   *tool.PlanModeState
	TaskList    *tool.TaskList
	Tracker     *engine.AgentRuntime
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
	if shared.CreateClient == nil {
		shared.CreateClient = func(string, string, string, string, string, string, string) agent.ModelClient {
			return nil
		}
	}
	if shared.ListAllModels == nil {
		shared.ListAllModels = func(context.Context) ([]protocol.ModelInfo, error) { return nil, nil }
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

	planState := tool.NewPlanModeState()
	planState.SetProjectDir(b.shared.ProjectDir)
	planState.SetSkillStore(b.shared.Skills)
	taskList := tool.NewTaskList()

	registry := b.buildRegistry(req.Profile, req.ToolNames, planState, taskList)
	assembler := b.buildAssembler(ctx, req, registry, contextWindow)
	eng := engine.NewEngine(client, registry, req.Yolo, b.shared.MaxTokens, assembler, b.shared.ProjectDir)
	eng.SetPlanModeState(planState)
	eng.SetTaskList(taskList)
	eng.SetStore(b.shared.Store)
	eng.SetModelInfo(req.Model, contextWindow)
	if req.DefaultMode != "" {
		eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionMode(req.DefaultMode)})
	}
	if req.Profile.Execution.DefaultEffort != "" {
		eng.SetEffort(req.Profile.Execution.DefaultEffort)
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
	if req.Profile.Tools.AllowAgentTool {
		registry.Register(tool.NewAgent(eng.AgentHandler()))
	}

	mediator := engine.NewEngineMediator(
		eng,
		b.shared.Store,
		b.shared.ProviderResolver,
		b.shared.CreateClient,
		b.shared.ListAllModels,
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
		SessionStore: b.shared.Store,
	}

	if req.Profile.Name == ProfileWorker {
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
	)
	return built, nil
}

func (b *Builder) buildRegistry(profile AgentProfile, toolNames []string, planState *tool.PlanModeState, taskList *tool.TaskList) *tool.Registry {
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
		tool.NewSkillTool(b.shared.Skills),
		tool.NewTodo(taskList),
	)
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

	excluded := map[string]struct{}{}
	if !profile.Tools.AllowAgentTool {
		excluded[tool.AgentToolName] = struct{}{}
	}
	for _, name := range []string{"Compact", "TrimToolResults", "Prune"} {
		excluded[name] = struct{}{}
	}

	if len(toolNames) == 0 {
		return cloneRegistryWithout(registry, excluded, b.shared.LintConfig, b.shared.ProjectDir)
	}
	selected := tool.NewRegistry()
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
		if req.Profile.Prompt.UseSubAgentPrompt {
			stable = prompt.FormatSubAgentSystemPrompt(b.shared.ProjectDir, req.SystemPromptExtra)
		} else {
			stable = prompt.FormatStableSystemPrompt(b.shared.ProjectDir)
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
```

- [ ] **Step 4: 让遗留 `runtime.Build(...)` 与 `subAgentFactory` 走 builder**

把 `internal/runtime/runtime.go` 的 `Build(...)` 和 `subAgentFactory` 改成下面的形状：

```go
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
		ProjectDir:       opts.ProjectDir,
		Store:            opts.Store,
		Skills:           skillStore,
		ExtraTools:       opts.ExtraTools,
		MCPManager:       opts.MCPManager,
		ProviderResolver: opts.ProviderResolver,
		CreateClient:     opts.CreateClientFn,
		ListAllModels:    opts.ListAllModelsFn,
		ContextWindowFor: opts.ContextWindowFor,
		ModelClientFor:   opts.ModelClientFor,
		LightClientFn:    opts.LightClientFn,
		MaxTokens:        opts.MaxTokens,
		LintConfig:       opts.LintConfig,
	})

	built, err := builder.Build(context.Background(), BuildRequest{
		ID:            "interactive-root",
		Model:         opts.Model,
		ContextWindow: opts.ContextWindow,
		ModelClient:   opts.ModelClient,
		Profile:       MustProfile(ProfileInteractive),
		Yolo:          opts.Yolo,
		DefaultMode:   opts.DefaultMode,
		StablePrompt:  opts.StablePrompt,
	})
	if err != nil {
		return nil, err
	}

	built.Engine.SetAgentController(nil)

	return &Bundle{
		Engine:    built.Engine,
		Mediator:  built.Mediator,
		Store:     opts.Store,
		Skills:    skillStore,
		PlanState: built.PlanState,
		TaskList:  built.TaskList,
		Registry:  built.Registry,
		Assembler: built.Assembler,
		Cleanup:   cleanup,
	}, nil
}

type subAgentFactory struct {
	builder          *Builder
	projectDir       string
	parentEng        *engine.Engine
	modelClientFor   func(model string) agent.ModelClient
	contextWindowFor ContextWindowFn
}

func (f *subAgentFactory) NewSubAgentRuntime(ctx context.Context, cfg engine.SubAgentBuildConfig) (*engine.AgentRuntime, error) {
	subModel := cfg.Model
	if subModel == "" {
		subModel = f.parentEng.SessionMetaModel()
	}
	contextWindow := 200000
	if f.contextWindowFor != nil {
		if cw := f.contextWindowFor(subModel); cw > 0 {
			contextWindow = cw
		}
	}
	built, err := f.builder.Build(ctx, BuildRequest{
		ID:                cfg.AgentID,
		Description:       cfg.Description,
		Model:             subModel,
		ContextWindow:     contextWindow,
		ModelClient:       f.modelClientFor(subModel),
		Profile:           MustProfile(ProfileWorker),
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
```

`runtime.Build(...)` 里创建 `subAgentFactory` 的地方，改为给它注入同一个 `builder`。

- [ ] **Step 5: 跑 builder 相关测试**

Run:

```bash
go test ./internal/runtime -run 'TestBuilderBuildsInteractiveAndWorkerRuntimes|TestBuildUsesBuilderForInteractiveBundle' -count=1
```

Expected: PASS.

- [ ] **Step 6: 提交这一小步**

```bash
git add internal/runtime/builder.go internal/runtime/builder_test.go internal/runtime/runtime.go
git commit -m "refactor: extract unified runtime builder

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

### Task 3: 新增 `Orchestrator`，把 worker control plane 从 `Engine` 中抽出去

**Files:**
- Create: `internal/engine/orchestrator.go`
- Create: `internal/engine/orchestrator_test.go`
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: 先写 orchestrator 的失败测试**

在 `internal/engine/orchestrator_test.go` 新建以下测试：

```go
package engine

import (
	"context"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/testkit"
	"github.com/zhanglvtao/cece/internal/tool"
)

type fakeRuntimeFactory struct {
	rt  *AgentRuntime
	err error
}

func (f *fakeRuntimeFactory) NewSubAgentRuntime(context.Context, SubAgentBuildConfig) (*AgentRuntime, error) {
	return f.rt, f.err
}

func TestEngineAgentHandlerDelegatesToAgentController(t *testing.T) {
	eng := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	eng.SetAgentController(agentControllerFunc(func(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
		return tool.AgentSubAgentResult{AgentID: "agent-1", Status: string(AgentStatusRunning), Content: "started"}, nil
	}))

	result, err := eng.AgentHandler().RunSubAgent(context.Background(), tool.AgentSubAgentConfig{Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("RunSubAgent error = %v", err)
	}
	if result.AgentID != "agent-1" {
		t.Fatalf("AgentID = %q, want agent-1", result.AgentID)
	}
	if result.Status != string(AgentStatusRunning) {
		t.Fatalf("Status = %q, want %q", result.Status, AgentStatusRunning)
	}
}

func TestOrchestratorStartReturnsImmediately(t *testing.T) {
	block := make(chan struct{})
	workerClient := &blockingClient{unblock: block}
	workerEngine := NewEngine(workerClient, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	start := time.Now()
	result, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("start should return immediately without waiting for worker completion")
	}
	if result.Status != string(AgentStatusRunning) && result.Status != string(AgentStatusStarting) {
		t.Fatalf("Status = %q, want starting/running", result.Status)
	}
	close(block)
}

func TestOrchestratorStatusAndCancel(t *testing.T) {
	workerClient := testkit.NewScriptedClient(testkit.TextTurn("done"))
	workerEngine := NewEngine(workerClient, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	_, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	status, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "status", AgentID: "agent-1"}, nil)
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if status.AgentID != "agent-1" {
		t.Fatalf("status.AgentID = %q, want agent-1", status.AgentID)
	}
	cancelled, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "cancel", AgentID: "agent-1"}, nil)
	if err != nil {
		t.Fatalf("cancel error = %v", err)
	}
	if !cancelled.Cancelled {
		t.Fatal("cancel should mark result as cancelled")
	}
}
```

在 `internal/engine/engine.go` 同时加上这个小 helper，供测试和生产共用：

```go
type AgentController interface {
	Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error)
}

type agentControllerFunc func(context.Context, *Engine, tool.AgentSubAgentConfig, tool.Emitter) (tool.AgentSubAgentResult, error)

func (f agentControllerFunc) Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
	return f(ctx, parent, cfg, emitter)
}
```

- [ ] **Step 2: 运行 orchestrator 测试，确认失败**

Run:

```bash
go test ./internal/engine -run 'TestEngineAgentHandlerDelegatesToAgentController|TestOrchestratorStartReturnsImmediately|TestOrchestratorStatusAndCancel' -count=1
```

Expected: FAIL，报 `undefined: NewOrchestrator` 或 `Engine.SetAgentController` 一类错误。

- [ ] **Step 3: 让 `Engine.AgentHandler()` 改为只委托控制器**

把 `internal/engine/engine.go` 里的相关字段与 `AgentHandler()` 改成下面这样：

```go
type Engine struct {
	mu         sync.Mutex
	client     agent.ModelClient
	registry   *tool.Registry
	assembler  *prompt.ContextAssembler
	projectDir string
	planState  *tool.PlanModeState
	taskList   *tool.TaskList
	history    []agent.Message
	cancel     context.CancelFunc
	confirmCh  chan struct{}
	rejectCh   chan struct{}
	yolo       bool
	maxTokens  int
	effort     string

	ContextWindowFor           func(model string) int
	ModelClientFor             func(model string) agent.ModelClient
	store                      session.Store
	sessionID                  string
	sessionCreated             bool
	modelName                  string
	contextWindow              int
	protocol                   string
	configName                 string
	lastInputTokens            int
	totalInputTokens           int
	totalOutputTokens          int
	apiCalls                   int
	toolCounts                 map[string]int
	failedToolCounts           map[string]int
	turnCount                  int
	cacheReadTokens            int
	cacheCreationTokens        int
	lastCompactTurn            int
	consecutiveCompactFailures int
	lastNudgeTurn              int
	inputQueue                 *userInputQueue
	questionAnswers            []tool.QuestionAnswer
	agentController            AgentController
	eventCh                    chan protocol.Event
}

func (e *Engine) SetAgentController(controller AgentController) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentController = controller
}

func (e *Engine) AgentHandler() *tool.AgentHandler {
	return &tool.AgentHandler{
		RunSubAgent: func(ctx context.Context, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
			e.mu.Lock()
			controller := e.agentController
			e.mu.Unlock()
			if controller == nil {
				return tool.AgentSubAgentResult{
					Status:  string(AgentStatusFailed),
					Content: "agent controller is not configured",
					Err:     "agent_controller_not_configured",
				}, nil
			}
			return controller.Run(ctx, e, cfg, emitter)
		},
	}
}
```

- [ ] **Step 4: 新建 `Orchestrator`，承接 start/status/send/answer/confirm/reject/cancel/switch_model**

在 `internal/engine/orchestrator.go` 写入下面的实现骨架，并把短方法按这个版本落地：

```go
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type PendingKind string

const (
	PendingNone    PendingKind = ""
	PendingQuestion PendingKind = "question"
	PendingConfirm  PendingKind = "confirm"
	PendingPlan     PendingKind = "plan"
)

type PendingState struct {
	Kind      PendingKind
	RequestID string
	Summary   string
}

type Orchestrator struct {
	mu      sync.Mutex
	factory SubAgentRuntimeFactory
	store   session.Store
	emit    func(protocol.Event)
	agents  map[string]*AgentRuntime
	pending map[string]PendingState
	nextID  int
}

func NewOrchestrator(factory SubAgentRuntimeFactory, store session.Store, emit func(protocol.Event)) *Orchestrator {
	return &Orchestrator{
		factory: factory,
		store:   store,
		emit:    emit,
		agents:  make(map[string]*AgentRuntime),
		pending: make(map[string]PendingState),
	}
}

func (o *Orchestrator) Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
	switch cfg.Operation {
	case "", "start":
		return o.start(ctx, parent, cfg)
	case "status":
		return o.status(cfg)
	case "send":
		return o.send(cfg)
	case "answer":
		return o.answer(cfg)
	case "confirm":
		return o.confirm(cfg)
	case "reject":
		return o.reject(cfg)
	case "cancel":
		return o.cancel(cfg)
	case "switch_model":
		return o.switchModel(cfg)
	default:
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("unknown operation: %s", cfg.Operation), Err: "unknown_operation"}, nil
	}
}

func (o *Orchestrator) start(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	parentSessionID := parent.SessionID()
	parentModel := parent.SessionMetaModel()

	o.mu.Lock()
	o.nextID++
	agentID := fmt.Sprintf("agent-%d", o.nextID)
	o.mu.Unlock()

	rt, err := o.factory.NewSubAgentRuntime(ctx, SubAgentBuildConfig{
		AgentID:           agentID,
		Description:       cfg.Description,
		Model:             cfg.Model,
		ParentSessionID:   parentSessionID,
		SystemPromptExtra: cfg.SystemPromptExtra,
		Tools:             cfg.Tools,
		MaxTurns:          cfg.MaxTurns,
	})
	if err != nil {
		slog.Error("orchestrator: worker build failed", "agent_id", agentID, "error", err)
		if o.emit != nil {
			o.emit(protocol.SubAgentFailedEvent{ID: agentID, Description: cfg.Description, ParentSessionID: parentSessionID, Error: err.Error()})
		}
		return tool.AgentSubAgentResult{AgentID: agentID, Status: string(AgentStatusFailed), Content: fmt.Sprintf("worker build failed: %v", err), Err: err.Error()}, err
	}

	if cfg.Model == "" {
		rt.mu.Lock()
		rt.Model = parentModel
		rt.mu.Unlock()
	}
	rt.Engine.SetEffort("low")

	o.mu.Lock()
	o.agents[agentID] = rt
	o.mu.Unlock()

	if o.emit != nil {
		o.emit(protocol.SubAgentStartedEvent{ID: agentID, Description: cfg.Description, ParentSessionID: parentSessionID})
	}
	go o.bridgeRuntime(parent, rt, parentSessionID)

	if err := rt.Engine.Input(rt.Context, cfg.Prompt); err != nil {
		rt.Cancel()
		if o.emit != nil {
			o.emit(protocol.SubAgentFailedEvent{ID: agentID, Description: cfg.Description, ParentSessionID: parentSessionID, Error: err.Error()})
		}
		return tool.AgentSubAgentResult{AgentID: agentID, Status: string(AgentStatusFailed), Content: fmt.Sprintf("worker start failed: %v", err), Err: err.Error()}, err
	}

	snap := rt.Snapshot()
	slog.Info("orchestrator: worker started", "agent_id", agentID, "profile", "worker", "parent_session_id", parentSessionID, "status", snap.Status)
	return tool.AgentSubAgentResult{
		AgentID:   agentID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   fmt.Sprintf("Agent %s started asynchronously. Use Agent status to poll progress.", agentID),
	}, nil
}

func (o *Orchestrator) status(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	snap := rt.Snapshot()
	msg := rt.LastAgentMessage()
	return tool.AgentSubAgentResult{
		AgentID:   rt.ID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   formatAgentMessage(msg),
	}, nil
}

func (o *Orchestrator) send(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if cfg.Input == "" {
		return tool.AgentSubAgentResult{Content: "input is required for send operation", Err: "missing_input"}, nil
	}
	rt.Engine.QueueInput(cfg.Input)
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("input queued for agent %s", rt.ID)}, nil
}

func (o *Orchestrator) answer(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	answers := make([]protocol.QuestionAnswer, len(cfg.Answers))
	for i, a := range cfg.Answers {
		answers[i] = protocol.QuestionAnswer{Question: a.Question, SelectedOptions: a.Selected, CustomText: a.Custom}
	}
	rt.Engine.AnswerQuestion(answers)
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("answers submitted to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) confirm(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	rt.Engine.Confirm()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("confirmation sent to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) reject(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	rt.Engine.RejectToolCalls()
	rt.Engine.RejectPlan()
	rt.Engine.RejectQuestion()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("rejection sent to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) cancel(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	rt.Cancel()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("agent %s cancelled", rt.ID), Cancelled: true}, nil
}

func (o *Orchestrator) switchModel(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if cfg.Model == "" {
		return tool.AgentSubAgentResult{Content: "model is required for switch_model operation", Err: "missing_model"}, nil
	}
	if rt.Mediator == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s has no mediator (switch_model not available)", rt.ID), Err: "no_mediator"}, nil
	}
	rt.Mediator.Do(protocol.SwitchModelAction{Model: cfg.Model})
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("agent %s switched to model %s", rt.ID, cfg.Model)}, nil
}

func (o *Orchestrator) bridgeRuntime(parent *Engine, rt *AgentRuntime, parentSessionID string) {
	for ev := range rt.Engine.Events() {
		msg, handled := rt.handleEvent(ev)
		if !handled {
			continue
		}
		rt.record(msg)
		if _, ok := ev.(protocol.SessionCreated); ok {
			updateSubAgentRelation(context.Background(), o.store, rt.SessionID, parentSessionID, rt.ID)
		}
		snap := rt.Snapshot()
		activity := snap.LastActivity
		if activity == "" {
			switch msg.Status {
			case AgentStatusWaitingInput:
				activity = "waiting for answer"
			case AgentStatusWaitingConfirm:
				activity = "waiting for confirmation"
			case AgentStatusWaitingPlan:
				activity = "waiting for plan approval"
			case AgentStatusCompleted:
				activity = "completed"
			case AgentStatusCancelled:
				activity = "cancelled"
			case AgentStatusFailed:
				activity = "failed"
			default:
				activity = "running"
			}
		}
		if o.emit != nil {
			o.emit(protocol.SubAgentActivityEvent{
				ID:               rt.ID,
				SessionID:        snap.SessionID,
				ParentSessionID:  parentSessionID,
				Activity:         activity,
				Status:           string(snap.Status),
				Model:            snap.Model,
				InputTokens:      snap.InputTokens,
				OutputTokens:     snap.OutputTokens,
				CacheReadTokens:  snap.CacheReadTokens,
				TurnCount:        snap.TurnCount,
				ToolCall:         snap.LastTool,
				LastAssistantMsg: snap.LastMessage,
			})
		}
		slog.Info("orchestrator: state transition", "agent_id", rt.ID, "status", snap.Status, "activity", activity)
		if rt.MaxTurns > 0 && rt.TurnCount >= rt.MaxTurns {
			rt.Result = tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(AgentStatusCompleted), Content: snap.LastMessage, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount, HitMaxTurns: true}
			if o.emit != nil {
				o.emit(protocol.SubAgentCompletedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount, HitMaxTurns: true})
			}
			parent.accumulateSubAgentTokens(rt.Result)
			rt.Engine.Cancel()
			return
		}
		switch msg.Status {
		case AgentStatusCompleted:
			result := parent.writeSubAgentArtifact(rt.resultFromMessage(msg), rt)
			parent.accumulateSubAgentTokens(result)
			if o.emit != nil {
				o.emit(protocol.SubAgentCompletedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount})
			}
		case AgentStatusFailed, AgentStatusCancelled:
			errText := ""
			if msg.Status == AgentStatusCancelled {
				errText = "cancelled"
			} else if p, ok := msg.Payload.(map[string]any); ok {
				if s, ok := p["error"].(string); ok {
					errText = s
				}
			}
			if o.emit != nil {
				o.emit(protocol.SubAgentFailedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, Error: errText})
			}
		}
	}
	rt.mu.Lock()
	if rt.FinishedAt.IsZero() {
		rt.FinishedAt = time.Now()
	}
	rt.mu.Unlock()
}

func (o *Orchestrator) get(agentID string) *AgentRuntime {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.agents[agentID]
}
```

在 `internal/engine/engine.go` 里，删掉下面这些旧方法与字段：

- `subAgents`
- `nextSubAgentID`
- `subAgentFactory`
- `runSubAgent`
- `startSubAgent`
- `startSubAgentBare`
- `statusSubAgent`
- `sendToSubAgent`
- `answerSubAgent`
- `confirmSubAgent`
- `rejectSubAgent`
- `cancelSubAgent`
- `switchSubAgentModel`
- `getSubAgent`
- `cleanupFinishedSubAgents`
- `BuildSubRegistry`
- `bridgeSubRuntimeEvents`
- `SetSubAgentFactory`

保留并继续复用：

- `writeSubAgentArtifact`
- `accumulateSubAgentTokens`
- `SessionID()` / `SessionMetaModel()` / `EmitEvent(...)`

同时把 `Input(...)` 开头的 `e.cleanupFinishedSubAgents()` 删除掉，因为 registry 现在归 orchestrator 所有。

- [ ] **Step 5: 重新运行 engine 测试**

Run:

```bash
go test ./internal/engine -run 'TestEngineAgentHandlerDelegatesToAgentController|TestOrchestratorStartReturnsImmediately|TestOrchestratorStatusAndCancel' -count=1
```

Expected: PASS.

- [ ] **Step 6: 提交这一小步**

```bash
git add internal/engine/engine.go internal/engine/orchestrator.go internal/engine/orchestrator_test.go
git commit -m "refactor: move worker orchestration out of engine

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

### Task 4: 把 `Agent` tool 切到异步 control plane，并补 async 回归测试

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/tool/agent_tool.go`
- Create: `internal/tool/agent_tool_test.go`
- Modify: `internal/testkit/e2e_advanced_test.go`
- Modify: `internal/testkit/e2e_scenarios_test.go`

- [ ] **Step 1: 先写失败的 Agent tool / async e2e 测试**

在 `internal/tool/agent_tool_test.go` 写入：

```go
package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolStartReturnsRunningStatusWithoutError(t *testing.T) {
	tool := NewAgent(&AgentHandler{
		RunSubAgent: func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error) {
			return AgentSubAgentResult{AgentID: "agent-1", Status: "running", Content: "Agent agent-1 started asynchronously."}, nil
		},
	})
	input, _ := json.Marshal(map[string]any{"operation": "start", "prompt": "inspect code", "description": "inspect code"})
	result := tool.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "agent-1") {
		t.Fatalf("content = %q, want agent id", result.Content)
	}
	if !strings.Contains(result.Content, "asynchronously") {
		t.Fatalf("content = %q, want async hint", result.Content)
	}
}

func TestAgentToolDescriptionMentionsAsyncControlPlane(t *testing.T) {
	tool := NewAgent(&AgentHandler{})
	desc := tool.Info().Description
	for _, want := range []string{"asynchronously", "status", "send", "answer", "confirm", "reject", "cancel"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q: %q", want, desc)
		}
	}
}
```

把 `internal/testkit/e2e_advanced_test.go` 里的旧 `TestE2E_SubAgent_CompletesTask` 替换为：

```go
func TestE2E_AgentStartThenStatusCompletesTask(t *testing.T) {
	workerLLM := testkit.NewScriptedClient(
		testkit.ToolUseTurn("worker-bash", "Bash", `{"command":"echo hello"}`),
		testkit.TextTurn("worker done: hello"),
	)
	parentLLM := testkit.NewScriptedClient(
		testkit.ToolUseTurn("agent-start", "Agent", `{"operation":"start","prompt":"analyze files","description":"file analysis","model":"sub-model","max_turns":5}`),
		testkit.ToolUseTurn("agent-status", "Agent", `{"operation":"status","agent_id":"agent-1"}`),
		testkit.TextTurn("received async worker result"),
	)
	fakeBash := testkit.NewFakeBash(map[string]testkit.CommandResult{
		"echo hello": {Stdout: "hello"},
	})

	h := testkit.NewHarness(t, parentLLM,
		testkit.WithCreateClientFn(func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient {
			return workerLLM
		}),
		testkit.WithExtraTools(fakeBash),
	)

	h.Send("analyze the codebase")
	testkit.WaitForEvent[protocol.SubAgentStartedEvent](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.SubAgentCompletedEvent](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if parentLLM.Calls() != 3 {
		t.Fatalf("parent LLM calls = %d, want 3", parentLLM.Calls())
	}
	if workerLLM.Calls() != 2 {
		t.Fatalf("worker LLM calls = %d, want 2", workerLLM.Calls())
	}
}
```

在 `internal/testkit/e2e_scenarios_test.go` 追加取消回归：

```go
func TestE2E_AgentCancelStopsRunningWorker(t *testing.T) {
	block := make(chan struct{})
	workerLLM := testkit.NewScriptedClient(testkit.ScriptedTurn{Block: block, Text: "never reached"})
	parentLLM := testkit.NewScriptedClient(
		testkit.ToolUseTurn("agent-start", "Agent", `{"operation":"start","prompt":"slow work","description":"slow work"}`),
		testkit.ToolUseTurn("agent-cancel", "Agent", `{"operation":"cancel","agent_id":"agent-1"}`),
		testkit.TextTurn("worker cancelled"),
	)

	h := testkit.NewHarness(t, parentLLM,
		testkit.WithCreateClientFn(func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient {
			return workerLLM
		}),
	)

	h.Send("run a slow worker")
	testkit.WaitForEvent[protocol.SubAgentStartedEvent](t, h, nil, 5*time.Second)
	close(block)
	testkit.WaitForEvent[protocol.SubAgentFailedEvent](t, h, func(e protocol.SubAgentFailedEvent) bool {
		return e.Error == "cancelled"
	}, 5*time.Second)
}
```

- [ ] **Step 2: 跑这些测试，确认失败**

Run:

```bash
go test ./internal/tool ./internal/testkit -run 'TestAgentToolStartReturnsRunningStatusWithoutError|TestAgentToolDescriptionMentionsAsyncControlPlane|TestE2E_AgentStartThenStatusCompletesTask|TestE2E_AgentCancelStopsRunningWorker' -count=1
```

Expected: FAIL，因为当前 `runtime.Build(...)` 还没有给 interactive engine 注入 orchestrator，而且 `Agent` tool 描述仍是同步 sub-agent 心智。

- [ ] **Step 3: 在 interactive runtime 构建阶段注入 orchestrator**

在 `internal/runtime/runtime.go` 里，interactive bundle 创建完成后，把 builder + orchestrator 接到 `Engine`：

```go
skillStore := opts.Skills
if skillStore == nil {
	skillStore = skill.NewStore(nil)
}

builder := NewBuilder(SharedDeps{
	ProjectDir:       opts.ProjectDir,
	Store:            opts.Store,
	Skills:           skillStore,
	ExtraTools:       opts.ExtraTools,
	MCPManager:       opts.MCPManager,
	ProviderResolver: opts.ProviderResolver,
	CreateClient:     opts.CreateClientFn,
	ListAllModels:    opts.ListAllModelsFn,
	ContextWindowFor: opts.ContextWindowFor,
	ModelClientFor:   opts.ModelClientFor,
	LightClientFn:    opts.LightClientFn,
	MaxTokens:        opts.MaxTokens,
	LintConfig:       opts.LintConfig,
})

built, err := builder.Build(context.Background(), BuildRequest{
	ID:            "interactive-root",
	Model:         opts.Model,
	ContextWindow: opts.ContextWindow,
	ModelClient:   opts.ModelClient,
	Profile:       MustProfile(ProfileInteractive),
	Yolo:          opts.Yolo,
	DefaultMode:   opts.DefaultMode,
	StablePrompt:  opts.StablePrompt,
})
if err != nil {
	return nil, err
}

built.Engine.SetAgentController(engine.NewOrchestrator(
	&subAgentFactory{
		builder:          builder,
		projectDir:       opts.ProjectDir,
		parentEng:        built.Engine,
		modelClientFor:   opts.ModelClientFor,
		contextWindowFor: opts.ContextWindowFor,
	},
	opts.Store,
	built.Engine.EmitEvent,
))
```

同时删掉任何还在设置 `SetSubAgentFactory(...)` 的旧路径。

- [ ] **Step 4: 更新 `Agent` tool 描述与 running/status 结果格式**

把 `internal/tool/agent_tool.go` 的描述文本替换为：

```go
Description: "Control another agent runtime asynchronously. Use operation=start to launch a worker and get its agent_id immediately. Then use status, send, answer, confirm, reject, switch_model, or cancel to drive it. Multiple Agent calls in a single response still run in parallel. Worker agents share the project directory and cannot spawn further agents.",
```

把 `Run(...)` 里的非完成态返回改成：

```go
	if result.Status != "" && result.Status != "completed" {
		var b strings.Builder
		b.WriteString(result.Content)
		if result.AgentID != "" {
			b.WriteString(fmt.Sprintf("\n\nAgent: %s", result.AgentID))
			if result.SessionID != "" {
				b.WriteString(fmt.Sprintf(" | Session: %s", result.SessionID))
			}
		}
		if result.Status != "" {
			b.WriteString(fmt.Sprintf("\nStatus: %s", result.Status))
		}
		return Result{Content: b.String()}
	}
```

保留 completed 分支里的 artifact / tokens 输出格式不变。

- [ ] **Step 5: 重新运行 Agent tool + async e2e 测试**

Run:

```bash
go test ./internal/tool ./internal/testkit -run 'TestAgentToolStartReturnsRunningStatusWithoutError|TestAgentToolDescriptionMentionsAsyncControlPlane|TestE2E_AgentStartThenStatusCompletesTask|TestE2E_AgentCancelStopsRunningWorker' -count=1
```

Expected: PASS.

- [ ] **Step 6: 提交这一小步**

```bash
git add internal/runtime/runtime.go internal/tool/agent_tool.go internal/tool/agent_tool_test.go internal/testkit/e2e_advanced_test.go internal/testkit/e2e_scenarios_test.go
git commit -m "feat: make agent orchestration async

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

### Task 5: 引入 `RuntimeHost`，让 stdio 根对象从 mediator 升级为 host

**Files:**
- Create: `internal/runtime/host.go`
- Create: `internal/runtime/host_test.go`
- Modify: `cmd/cece/main.go`
- Modify: `internal/testkit/harness.go`

- [ ] **Step 1: 先写 host 的失败测试**

在 `internal/runtime/host_test.go` 写入：

```go
package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/testkit"
)

func TestHostDelegatesInputAndEventsToForegroundRuntime(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("hello from host"))
	host, err := NewHost(context.Background(), Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   llm,
		Store:         testkit.NewMemStore(),
	})
	if err != nil {
		t.Fatalf("NewHost error = %v", err)
	}
	if err := host.Input(context.Background(), "ping"); err != nil {
		t.Fatalf("host.Input error = %v", err)
	}
	select {
	case ev := <-host.Events():
		if _, ok := ev.(protocol.SessionCreated); !ok {
			t.Fatalf("first event = %T, want protocol.SessionCreated", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for host events")
	}
}

func TestHostDoDelegatesToMediator(t *testing.T) {
	llm := testkit.NewScriptedClient()
	host, err := NewHost(context.Background(), Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   llm,
		Store:         testkit.NewMemStore(),
	})
	if err != nil {
		t.Fatalf("NewHost error = %v", err)
	}
	host.Do(protocol.ListModelsAction{})
	select {
	case ev := <-host.Events():
		if _, ok := ev.(protocol.ModelsLoadedEvent); !ok {
			// 允许先读到 EngineReadyEvent，再继续等模型列表事件。
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for model list event")
	}
}
```

- [ ] **Step 2: 运行 host 测试，确认失败**

Run:

```bash
go test ./internal/runtime -run 'TestHostDelegatesInputAndEventsToForegroundRuntime|TestHostDoDelegatesToMediator' -count=1
```

Expected: FAIL，报 `undefined: NewHost` 一类错误。

- [ ] **Step 3: 实现 `RuntimeHost`**

在 `internal/runtime/host.go` 新建下面的实现：

```go
package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/skill"
)

type Host struct {
	interactive  *BuiltRuntime
	orchestrator *engine.Orchestrator
	cleanup      func()
}

func NewHost(ctx context.Context, opts Options) (*Host, error) {
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
		ProjectDir:       opts.ProjectDir,
		Store:            opts.Store,
		Skills:           skillStore,
		ExtraTools:       opts.ExtraTools,
		MCPManager:       opts.MCPManager,
		ProviderResolver: opts.ProviderResolver,
		CreateClient:     opts.CreateClientFn,
		ListAllModels:    opts.ListAllModelsFn,
		ContextWindowFor: opts.ContextWindowFor,
		ModelClientFor:   opts.ModelClientFor,
		LightClientFn:    opts.LightClientFn,
		MaxTokens:        opts.MaxTokens,
		LintConfig:       opts.LintConfig,
	})

	interactive, err := builder.Build(ctx, BuildRequest{
		ID:            "interactive-root",
		Model:         opts.Model,
		ContextWindow: opts.ContextWindow,
		ModelClient:   opts.ModelClient,
		Profile:       MustProfile(ProfileInteractive),
		Yolo:          opts.Yolo,
		DefaultMode:   opts.DefaultMode,
		StablePrompt:  opts.StablePrompt,
	})
	if err != nil {
		return nil, err
	}

	orch := engine.NewOrchestrator(
		&subAgentFactory{
			builder:          builder,
			projectDir:       opts.ProjectDir,
			parentEng:        interactive.Engine,
			modelClientFor:   opts.ModelClientFor,
			contextWindowFor: opts.ContextWindowFor,
		},
		opts.Store,
		interactive.Engine.EmitEvent,
	)
	interactive.Engine.SetAgentController(orch)

	h := &Host{
		interactive:  interactive,
		orchestrator: orch,
		cleanup:      cleanup,
	}
	slog.Info("runtime host: started", "model", opts.Model, "project_dir", opts.ProjectDir)
	return h, nil
}

func (h *Host) Input(ctx context.Context, input string) error {
	slog.Info("runtime host: input", "chars", len(input))
	return h.interactive.Mediator.Input(ctx, input)
}

func (h *Host) Do(action protocol.Action) {
	slog.Info("runtime host: action", "type", actionType(action))
	h.interactive.Mediator.Do(action)
}

func (h *Host) Events() <-chan protocol.Event { return h.interactive.Engine.Events() }

func (h *Host) Wait() {
	h.interactive.Mediator.Wait()
}

func (h *Host) Close() {
	if h.cleanup != nil {
		h.cleanup()
	}
}

func (h *Host) Engine() *engine.Engine { return h.interactive.Engine }
func (h *Host) Mediator() *engine.EngineMediator { return h.interactive.Mediator }

func actionType(action protocol.Action) string {
	return fmt.Sprintf("%T", action)
}
```

- [ ] **Step 4: 切换 CLI 和 testkit 到 host**

在 `cmd/cece/main.go`：

1. 把 `runtimeBundle` 改成：

```go
type runtimeBundle struct {
	host          *runtime.Host
	store         session.Store
	skillStore    *skill.Store
	model         string
	contextWindow int
	defaultMode   string
	cleanup       func()
}
```

2. 在 `buildRuntime(...)` 里用 `runtime.NewHost(...)` 代替 `runtime.Build(...)`：

```go
host, err := runtime.NewHost(ctx, runtime.Options{
	ProjectDir:       projectDir,
	Model:            cfg.Model,
	ContextWindow:    contextWindow,
	MaxTokens:        cfg.MaxTokens,
	Yolo:             cfg.Yolo,
	DefaultMode:      cfg.DefaultMode,
	LintConfig:       cfg.Lint,
	ModelClient:      client,
	Store:            store,
	Skills:           skillStore,
	ExtraTools:       nil,
	MCPManager:       mcpMgr,
	ProviderResolver: providerResolver,
	CreateClientFn:   createClientFn,
	ListAllModelsFn:  listAllModelsFn,
	ContextWindowFor: cfg.ContextWindowFor,
	ModelClientFor:   modelClientFor,
	LightClientFn:    lightClientFn,
})
if err != nil {
	return runtimeBundle{}, err
}

host.Engine().EmitEvent(protocol.EngineReadyEvent{Model: cfg.Model, ContextWindow: contextWindow})

return runtimeBundle{
	host:          host,
	store:         store,
	skillStore:    skillStore,
	model:         cfg.Model,
	contextWindow: contextWindow,
	defaultMode:   cfg.DefaultMode,
	cleanup:       host.Close,
}, nil
```

3. 在 `runEngineStdio(...)` 里改成：

```go
if err := ipc.Serve(ctx, bundle.host, os.Stdin, os.Stdout); err != nil {
	logger.Error("engine stdio exited", "error", err)
	return 1
}
```

在 `internal/testkit/harness.go` 里，把 `runtime.Build(...)` 改成 `runtime.NewHost(...)`，并把 `Harness` 的赋值改成：

```go
host, err := runtime.NewHost(context.Background(), runtime.Options{
	ProjectDir:       cfg.projectDir,
	Model:            cfg.model,
	ContextWindow:    cfg.contextWindow,
	MaxTokens:        cfg.maxTokens,
	Yolo:             cfg.yolo,
	DefaultMode:      cfg.defaultMode,
	StablePrompt:     "test stable prompt",
	ModelClient:      llm,
	Store:            cfg.store,
	Skills:           cfg.skills,
	ExtraTools:       cfg.extraTools,
	MCPManager:       cfg.mcpManager,
	ProviderResolver: cfg.providerResolver,
	CreateClientFn:   cfg.createClientFn,
	ModelClientFor:   modelClientFor,
	ListAllModelsFn:  cfg.listAllModelsFn,
	LightClientFn:    lightFn,
})
if err != nil {
	t.Fatalf("testkit harness build failed: %v", err)
}

h := &Harness{
	t:     t,
	LLM:   llm,
	Eng:   host.Engine(),
	Med:   host.Mediator(),
	Store: cfg.store,
}
```

- [ ] **Step 5: 跑 host + harness 回归**

Run:

```bash
go test ./internal/runtime ./internal/testkit -run 'TestHostDelegatesInputAndEventsToForegroundRuntime|TestHostDoDelegatesToMediator|TestE2E_AgentStartThenStatusCompletesTask|TestE2E_AgentCancelStopsRunningWorker' -count=1
```

Expected: PASS.

- [ ] **Step 6: 提交这一小步**

```bash
git add internal/runtime/host.go internal/runtime/host_test.go cmd/cece/main.go internal/testkit/harness.go
git commit -m "refactor: add runtime host

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

### Task 6: 补齐结构化日志并做最终回归

**Files:**
- Modify: `internal/runtime/builder.go`
- Modify: `internal/engine/orchestrator.go`
- Modify: `internal/runtime/host.go`
- Modify: `internal/engine/orchestrator_test.go`

- [ ] **Step 1: 先写一个 orchestrator 日志 smoke 测试**

在 `internal/engine/orchestrator_test.go` 追加：

```go
func TestOrchestratorLogsLifecycle(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(old)

	workerClient := testkit.NewScriptedClient(testkit.TextTurn("done"))
	workerEngine := NewEngine(workerClient, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	_, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "status", AgentID: "agent-1"}, nil); err != nil {
		t.Fatalf("status error = %v", err)
	}
	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "cancel", AgentID: "agent-1"}, nil); err != nil {
		t.Fatalf("cancel error = %v", err)
	}

	got := buf.String()
	for _, want := range []string{"orchestrator: worker started", "agent_id=agent-1", "orchestrator: state transition"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q: %s", want, got)
		}
	}
}
```

记得补 `bytes`、`log/slog`、`strings` imports。

- [ ] **Step 2: 运行日志测试，确认先失败**

Run:

```bash
go test ./internal/engine -run 'TestOrchestratorLogsLifecycle' -count=1
```

Expected: FAIL，因为当前日志点还不够完整，断言的字段不会全都出现。

- [ ] **Step 3: 补 builder / orchestrator / host 的结构化日志**

按下面这些日志点加 `slog.Info` / `slog.Warn`：

在 `internal/runtime/builder.go` 增加：

```go
slog.Info("runtime builder: build start",
	"runtime_id", req.ID,
	"profile", req.Profile.Name,
	"model", req.Model,
	"parent_session_id", req.ParentSessionID,
	"max_turns", req.MaxTurns,
)
```

放在 `Build(...)` 开头；在成功返回前保留已有 `runtime builder: build complete`，再补：

```go
slog.Info("runtime builder: registry ready",
	"runtime_id", req.ID,
	"profile", req.Profile.Name,
	"tool_count", len(registry.Definitions()),
	"agent_tool_enabled", req.Profile.Tools.AllowAgentTool,
)
```

在 `internal/engine/orchestrator.go` 的 `Run(...)` 入口加：

```go
slog.Info("orchestrator: operation",
	"operation", cfg.Operation,
	"agent_id", cfg.AgentID,
	"description", cfg.Description,
	"model", cfg.Model,
)
```

在 `status/send/answer/confirm/reject/cancel/switchModel` 每个分支成功返回前都加一条 `slog.Info(...)`，至少带：

```go
"operation", "status",
"agent_id", rt.ID,
"status", snap.Status,
```

在 `internal/runtime/host.go` 里保留已有 `runtime host: started` / `runtime host: input` / `runtime host: action`，再在 `Wait()` 里补：

```go
slog.Info("runtime host: shutdown complete")
```

- [ ] **Step 4: 跑 focused + package regression**

Run:

```bash
go test ./internal/engine -run 'TestOrchestratorLogsLifecycle|TestOrchestratorStartReturnsImmediately|TestOrchestratorStatusAndCancel' -count=1
go test ./internal/runtime ./internal/engine ./internal/tool ./internal/testkit -count=1
```

Expected: PASS.

- [ ] **Step 5: 跑全量回归**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: 检查 diff 并提交最终重构**

Run:

```bash
git diff -- internal/runtime/profile.go internal/runtime/profile_test.go internal/runtime/builder.go internal/runtime/builder_test.go internal/runtime/runtime.go internal/runtime/host.go internal/runtime/host_test.go internal/engine/engine.go internal/engine/orchestrator.go internal/engine/orchestrator_test.go internal/tool/agent_tool.go internal/tool/agent_tool_test.go internal/testkit/harness.go internal/testkit/e2e_advanced_test.go internal/testkit/e2e_scenarios_test.go cmd/cece/main.go docs/superpowers/plans/2026-06-17-unify-runtime-builder-and-orchestrator.md
```

Expected: diff 只包含 profile/builder/orchestrator/host 重构、异步 Agent tool 语义、日志补齐、测试与本计划文档。

然后提交：

```bash
git add internal/runtime/profile.go internal/runtime/profile_test.go internal/runtime/builder.go internal/runtime/builder_test.go internal/runtime/runtime.go internal/runtime/host.go internal/runtime/host_test.go internal/engine/engine.go internal/engine/orchestrator.go internal/engine/orchestrator_test.go internal/tool/agent_tool.go internal/tool/agent_tool_test.go internal/testkit/harness.go internal/testkit/e2e_advanced_test.go internal/testkit/e2e_scenarios_test.go cmd/cece/main.go docs/superpowers/plans/2026-06-17-unify-runtime-builder-and-orchestrator.md
git commit -m "refactor: unify runtime builder and agent orchestration

Co-Authored-By: Cece <zhanglvtao@foxmail.com>"
```

---

## Self-Review

- Spec coverage:
  - `AgentRuntimeBuilder + AgentProfile`：Task 1、Task 2
  - `Orchestrator` 抽离与 async control plane：Task 3、Task 4
  - `RuntimeHost` 作为 TUI / IPC 根对象：Task 5
  - 结构化日志与可观测性：Task 6
  - 测试与回归：Task 1-6 都包含 focused tests，Task 6 包含 `go test ./...`
- Placeholder scan:
  - 没有 `TBD`、`TODO`、`implement later`、`similar to` 这类占位语句。
- Type consistency:
  - 统一使用 `ProfileName` / `AgentProfile` / `BuildRequest` / `BuiltRuntime` / `AgentController` / `Orchestrator` / `Host`。
  - worker 状态继续复用现有 `AgentStatus*`，兼容现有 `SubAgent*Event`。
  - `Agent` tool 保留现有 schema 字段，`start/status/send/answer/confirm/reject/cancel/switch_model` 全部在 orchestrator 中落地。
