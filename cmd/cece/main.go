package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"

	"cece/internal/agent"
	"cece/internal/aiden"
	"cece/internal/claude"
	"cece/internal/codebase"
	"cece/internal/config"
	"cece/internal/daemon"
	"cece/internal/engine"
	"cece/internal/ipc"
	"cece/internal/lint"
	"cece/internal/logger"
	"cece/internal/mcp"
	"cece/internal/prompt"
	"cece/internal/protocol"
	"cece/internal/remote"
	"cece/internal/session"
	"cece/internal/skill"
	"cece/internal/tool"
	"cece/internal/ui"
)

type runtimeBundle struct {
	mediator      *engine.EngineMediator
	store         session.Store
	skillStore    *skill.Store
	model         string
	contextWindow int
	defaultMode   string
	cleanup       func()
}

type runtimeMetadata struct {
	model         string
	contextWindow int
	defaultMode   string
	debug         bool
}

func createClient(pc config.ProviderConfig, model string, configName string) agent.ModelClient {
	if configName == "" {
		for _, sm := range pc.Models {
			if sm.ID == model {
				configName = sm.ConfigName
				break
			}
		}
	}
	switch pc.Protocol {
	case "codebase":
		c := codebase.NewClient(pc.APIKey, model, configName, pc.BaseURL)
		if pc.AuthHelper != "" {
			c.SetAuthHelper(pc.AuthHelper)
		}
		return c
	case "aiden":
		c := aiden.NewClient(pc.APIKey, model, pc.BaseURL)
		if pc.AuthHelper != "" {
			c.SetAuthHelper(pc.AuthHelper)
		}
		return c
	default:
		c := claude.NewClient(pc.APIKey, model, pc.BaseURL, claude.ParseAuthMode(pc.AuthMode))
		c.SetThinking(true, 0)
		if pc.AuthHelper != "" {
			c.SetAuthHelper(pc.AuthHelper)
		}
		return c
	}
}

func main() {
	projectDir, _ := os.Getwd()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "engine":
			os.Exit(runEngineStdio(projectDir, os.Args[2:]))
		case "hub":
			os.Exit(runHub(projectDir, os.Args[2:]))
		case "help", "--help", "-h":
			printHelp()
			os.Exit(0)
		}
	}
	os.Exit(runTUI(projectDir))
}

func printHelp() {
	fmt.Print(`cece — AI coding agent

Usage:
  cece              Start interactive TUI
  cece hub <cmd>    Hub daemon commands
  cece engine       Run engine process (internal)

Commands:
  help              Show this help

Hub commands:
  cece hub start              Start hub daemon
  cece hub stop               Stop hub daemon
  cece hub status             Show hub status
  cece hub tui [session-id]   Open TUI connected to hub-managed engine
  cece hub session list       List all sessions
  cece hub session run <prompt>   Create session and send prompt
  cece hub session input <id> <text>  Send input to session
  cece hub session cancel <id>   Cancel running session
  cece hub session delete <id>   Delete session
`)
}

func runTUI(projectDir string) int {
	meta, err := loadMetadata(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer logger.Sync()

	client, err := remote.New(context.Background(), remote.Options{ProjectDir: projectDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine start failed: %v\n", err)
		return 1
	}
	defer client.Close()

	model := ui.NewModel(client, meta.model, projectDir, meta.contextWindow)
	model.SetDefaultMode(meta.defaultMode)
	model.SetSessions(session.NewFileStore(projectDir))
	model.SetSkillStore(skill.NewStore(skill.DiscoverAll(projectDir)))

	program := tea.NewProgram(&model)
	if _, err := program.Run(); err != nil {
		logger.Error("program exited", "error", err)
		return 1
	}
	return 0
}

func runEngineStdio(defaultProjectDir string, args []string) int {
	var projectDir string
	var socketPath string
	var sessionID string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project-dir":
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++
			}
		case "--stdio":
		case "--socket":
			if i+1 < len(args) {
				socketPath = args[i+1]
				i++
			}
		case "--session-id":
			if i+1 < len(args) {
				sessionID = args[i+1]
				i++
			}
		}
	}

	bundle, err := buildRuntime(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine init failed: %v\n", err)
		return 1
	}
	defer bundle.cleanup()

	// If a session-id is provided, load it immediately so the engine
	// starts with the correct history instead of a blank state.
	if sessionID != "" {
		bundle.mediator.Do(protocol.LoadSessionAction{SessionID: sessionID})
	}

	ctx := context.Background()

	if socketPath != "" {
		if err := ipc.ServeSocket(ctx, bundle.mediator, socketPath); err != nil {
			logger.Error("engine socket exited", "error", err)
			return 1
		}
		return 0
	}

	if err := ipc.Serve(ctx, bundle.mediator, os.Stdin, os.Stdout); err != nil {
		logger.Error("engine stdio exited", "error", err)
		return 1
	}
	return 0
}

func loadMetadata(projectDir string) (runtimeMetadata, error) {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return runtimeMetadata{}, fmt.Errorf("config load failed: %w", err)
	}
	logPath := filepath.Join(projectDir, ".cece", "cece.log")
	if err := logger.Init(logPath, cfg.Debug); err != nil {
		return runtimeMetadata{}, fmt.Errorf("logger init failed: %w", err)
	}
	logger.Info("cece tui starting", "model", cfg.Model)
	return runtimeMetadata{
		model:         cfg.Model,
		contextWindow: cfg.ContextWindowFor(cfg.Model),
		defaultMode:   cfg.DefaultMode,
		debug:         cfg.Debug,
	}, nil
}

func buildRuntime(projectDir string) (runtimeBundle, error) {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return runtimeBundle{}, fmt.Errorf("config load failed: %w", err)
	}

	logPath := filepath.Join(projectDir, ".cece", "cece.log")
	if err := logger.Init(logPath, cfg.Debug); err != nil {
		return runtimeBundle{}, fmt.Errorf("logger init failed: %w", err)
	}
	cleanup := func() { logger.Sync() }

	defaultProvider := cfg.Providers[0]
	logger.Info("cece engine starting", "model", cfg.Model, "provider", defaultProvider.Name, "maxTokens", cfg.MaxTokens)

	client := createClient(defaultProvider, cfg.Model, "")
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	skillStore := skill.NewStore(skill.DiscoverAll(projectDir))
	taskList := tool.NewTaskList()

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
		tool.NewTask(taskList),
	)

	if len(cfg.Lint) > 0 {
		registry.SetLinter(lint.NewRunner(cfg.Lint, projectDir))
	}

	ctx := context.Background()

	mcpMgr := mcp.NewManager()
	cleanup = func() {
		mcpMgr.Close()
		logger.Sync()
	}
	if len(cfg.MCP) > 0 {
		mcpMgr.Initialize(ctx, cfg.MCP)
		for _, t := range mcpMgr.Tools() {
			registry.Register(t)
		}
	}

	stablePrompt := prompt.FormatStableSystemPrompt(projectDir)
	collector := prompt.NewDefaultSessionCollector(projectDir, registry)
	collector.SetSkillProvider(skillStore)
	assembler := prompt.NewContextAssembler(stablePrompt, registry, collector)

	if _, err := assembler.RefreshSession(ctx); err != nil {
		logger.Warn("initial session refresh failed", "error", err)
	}

	var contextWindow int
	if lister, ok := client.(interface {
		GetModelInfo(context.Context) (agent.ModelInfo, error)
	}); ok {
		if info, err := lister.GetModelInfo(ctx); err != nil {
			logger.Warn("model info query failed, trying ListModels", "error", err)
			contextWindow = cfg.ContextWindowFor(cfg.Model)
		} else {
			contextWindow = info.MaxContextWindow
			logger.Info("model context window set from API", "max_context", contextWindow)
		}
	}
	if contextWindow <= 0 {
		if lister, ok := client.(interface {
			ListModels(context.Context) ([]agent.ModelInfo, error)
		}); ok {
			if models, err := lister.ListModels(ctx); err != nil {
				contextWindow = cfg.ContextWindowFor(cfg.Model)
				logger.Warn("ListModels failed, using config/default", "error", err, "context_window", contextWindow)
			} else {
				for _, m := range models {
					if m.ID == cfg.Model {
						contextWindow = m.MaxContextWindow
						break
					}
				}
				if contextWindow <= 0 {
					contextWindow = cfg.ContextWindowFor(cfg.Model)
				}
				logger.Info("model context window set from ListModels", "max_context", contextWindow)
			}
		} else {
			contextWindow = cfg.ContextWindowFor(cfg.Model)
			logger.Info("model context window from config", "max_context", contextWindow)
		}
	}
	assembler.SetMaxContextTokens(contextWindow)

	eng := engine.NewEngine(client, registry, cfg.Yolo, cfg.MaxTokens, assembler, projectDir)
	eng.SetPlanModeState(planState)
	eng.SetTaskList(taskList)
	eng.SetModelInfo(cfg.Model, contextWindow)
	registry.Register(tool.NewCompact(eng.CompactHandler()))
	eng.SetToolResultPolicy(agent.ToolResultPolicy{
		InlineMaxLines: cfg.ToolResult.InlineMaxLines,
		HeadLines:      cfg.ToolResult.HeadLines,
		TailLines:      cfg.ToolResult.TailLines,
	})
	eng.ContextWindowFor = cfg.ContextWindowFor

	store := session.NewFileStore(projectDir)
	eng.SetStore(store)

	createClientFn := func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient {
		pc := config.ProviderConfig{
			Protocol:   protocol,
			APIKey:     apiKey,
			BaseURL:    baseURL,
			AuthMode:   authMode,
			AuthHelper: authHelper,
		}
		return createClient(pc, model, configName)
	}

	providerResolver := func(configName string) (apiKey, baseURL, authMode, authHelper, protocol string) {
		for _, p := range cfg.Providers {
			if configName != "" {
				for _, m := range p.Models {
					if m.ConfigName == configName {
						return p.APIKey, p.BaseURL, p.AuthMode, p.AuthHelper, p.Protocol
					}
				}
			}
			if p.Name == configName {
				return p.APIKey, p.BaseURL, p.AuthMode, p.AuthHelper, p.Protocol
			}
		}
		if len(cfg.Providers) > 0 {
			p := cfg.Providers[0]
			return p.APIKey, p.BaseURL, p.AuthMode, p.AuthHelper, p.Protocol
		}
		return "", "", "", "", ""
	}

	if cfg.DefaultMode != "" {
		eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionMode(cfg.DefaultMode)})
	}

	listAllModelsFn := func(ctx context.Context) ([]protocol.ModelInfo, error) {
		var allModels []protocol.ModelInfo
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, p := range cfg.Providers {
			wg.Add(1)
			go func(pc config.ProviderConfig) {
				defer wg.Done()

				var models []agent.ModelInfo
				tmpClient := createClient(pc, "", "")
				if lister, ok := tmpClient.(interface {
					ListModels(context.Context) ([]agent.ModelInfo, error)
				}); ok {
					if result, err := lister.ListModels(ctx); err == nil {
						models = result
						for i := range models {
							if models[i].MaxContextWindow <= 0 {
								models[i].MaxContextWindow = cfg.ContextWindowFor(models[i].ID)
							}
						}
					} else {
						slog.Warn("provider ListModels failed, trying static list", "provider", pc.Name, "error", err)
					}
				}

				if len(models) == 0 && len(pc.Models) > 0 {
					models = make([]agent.ModelInfo, len(pc.Models))
					for i, sm := range pc.Models {
						models[i] = agent.ModelInfo{
							ID:               sm.ID,
							DisplayName:      sm.DisplayName,
							MaxContextWindow: sm.MaxContextWindow,
							ConfigName:       sm.ConfigName,
						}
					}
				}

				if len(models) == 0 {
					return
				}

				dtoModels := make([]protocol.ModelInfo, len(models))
				for i, m := range models {
					dtoModels[i] = protocol.ModelInfo{
						ID:               m.ID,
						DisplayName:      m.DisplayName,
						MaxContextWindow: m.MaxContextWindow,
						Provider:         pc.Name,
						APIKey:           pc.APIKey,
						BaseURL:          pc.BaseURL,
						AuthMode:         pc.AuthMode,
						AuthHelper:       pc.AuthHelper,
						Protocol:         pc.Protocol,
						ConfigName:       m.ConfigName,
					}
				}

				mu.Lock()
				allModels = append(allModels, dtoModels...)
				mu.Unlock()
			}(p)
		}
		wg.Wait()
		if len(allModels) == 0 {
			return nil, errors.New("no models available from any provider")
		}
		return allModels, nil
	}

	mediator := engine.NewEngineMediator(eng, store, providerResolver, createClientFn, listAllModelsFn, mcpMgr)
	return runtimeBundle{
		mediator:      mediator,
		store:         store,
		skillStore:    skillStore,
		model:         cfg.Model,
		contextWindow: contextWindow,
		defaultMode:   cfg.DefaultMode,
		cleanup:       cleanup,
	}, nil
}

// ── Hub subcommand ──────────────────────────────────────────────────────────

func runHub(projectDir string, args []string) int {
	if len(args) == 0 {
		args = []string{"start"}
	}
	switch args[0] {
	case "start":
		return runHubStart(projectDir)
	case "stop":
		return runHubStop()
	case "status":
		return runHubStatus()
	case "tui":
		sessionID := ""
		if len(args) > 1 {
			sessionID = args[1]
		}
		return runHubTUI(projectDir, sessionID)
	case "session":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cece hub session <list|run|input|cancel|delete>")
			return 1
		}
		return runHubSession(args[1], args[2:])
	case "help", "--help", "-h":
		printHubHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown hub command: %s\n", args[0])
		return 1
	}
}

func printHubHelp() {
	fmt.Print(`cece hub — session daemon

Usage:
  cece hub <command> [args]

Commands:
  start                Start hub daemon (blocks)
  stop                 Stop running hub daemon
  status               Show hub status
  tui [session-id]     Open TUI connected to hub-managed engine
                         No session-id: create new session
                         With session-id: attach to existing session

Session commands:
  cece hub session list                    List all sessions
  cece hub session run <prompt>            Create session and send prompt
  cece hub session input <id> <text>       Send input to a session
  cece hub session cancel <id>             Cancel running session
  cece hub session delete <id>             Delete a session
`)
}

func runHubStart(projectDir string) int {
	meta, err := loadMetadata(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	_ = meta

	hub, err := daemon.NewHub(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hub init failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "cece hub started on %s\n", hub.SocketPath())

	if err := hub.Run(); err != nil {
		logger.Error("hub exited", "error", err)
		return 1
	}
	return 0
}

func runHubStop() int {
	client := daemon.NewHubClient()
	if err := client.Shutdown(); err != nil {
		fmt.Fprintf(os.Stderr, "hub stop failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "hub stopped")
	return 0
}

func runHubStatus() int {
	client := daemon.NewHubClient()
	status, err := client.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hub not running: %v\n", err)
		return 1
	}
	fmt.Printf("running: %v\nproject: %s\nsocket: %s\nactive: %d\nsessions: %d\n",
		status.Running, status.ProjectDir, status.SocketPath, status.ActiveCount, status.SessionCount)
	return 0
}

func runHubTUI(projectDir, sessionID string) int {
	meta, err := loadMetadata(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer logger.Sync()

	client := daemon.NewHubClient()

	if sessionID == "" {
		sess, err := client.CreateSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "hub create session failed: %v\n", err)
			return 1
		}
		sessionID = sess.ID
	}

	remoteClient, err := daemon.DialEngineSocket(context.Background(), projectDir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to engine failed: %v\n", err)
		return 1
	}
	defer remoteClient.Close()

	model := ui.NewModel(remoteClient, meta.model, projectDir, meta.contextWindow)
	model.SetDefaultMode(meta.defaultMode)
	model.SetSessions(session.NewFileStore(projectDir))
	model.SetSkillStore(skill.NewStore(skill.DiscoverAll(projectDir)))

	program := tea.NewProgram(&model)
	if _, err := program.Run(); err != nil {
		logger.Error("program exited", "error", err)
		return 1
	}
	return 0
}

func runHubSession(subCmd string, args []string) int {
	client := daemon.NewHubClient()

	switch subCmd {
	case "list", "ls":
		sessions, err := client.ListSessions()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		for _, s := range sessions {
			fmt.Printf("%s  %-10s  %-8s  %s\n", s.ID[:8], s.Status, s.Source, s.Title)
		}
		return 0

	case "run":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: cece hub session run <prompt>")
			return 1
		}
		sess, err := client.CreateSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "create session failed: %v\n", err)
			return 1
		}
		if err := client.SendInput(sess.ID, args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "send input failed: %v\n", err)
			return 1
		}
		fmt.Printf("session %s created and running\n", sess.ID[:8])
		return 0

	case "input":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cece hub session input <id> <text>")
			return 1
		}
		if err := client.SendInput(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0

	case "cancel":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: cece hub session cancel <id>")
			return 1
		}
		if err := client.CancelSession(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0

	case "delete", "rm":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: cece hub session delete <id>")
			return 1
		}
		if err := client.DeleteSession(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("session %s deleted\n", args[0][:8])
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown session command: %s\n", subCmd)
		return 1
	}
}
