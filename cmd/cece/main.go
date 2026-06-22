package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/aiden"
	"github.com/zhanglvtao/cece/internal/claude"
	"github.com/zhanglvtao/cece/internal/codebase"
	"github.com/zhanglvtao/cece/internal/config"
	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/ipc"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/mcp"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/remote"
	"github.com/zhanglvtao/cece/internal/runtime"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/setup"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/ui"
	"github.com/zhanglvtao/cece/internal/update"
	"github.com/zhanglvtao/cece/internal/version"
)

type runtimeBundle struct {
	mediator      *engine.EngineMediator
	store         session.Store
	skillStore    *skill.Store
	model         string
	contextWindow int
	defaultMode   string
	defaultEffort string
	cleanup       func()
}

type runtimeMetadata struct {
	model         string
	contextWindow int
	defaultMode   string
	defaultEffort string
	debug         bool
	enabledSkills []string
}

// shouldUseResponsesAPI returns true for OpenAI models that should use the
// Responses API (/v1/responses) instead of Chat Completions (/v1/chat/completions).
// This bypasses Aiden proxy's buggy g() conversion which incorrectly marks
// assistant text as "input_text" instead of "output_text".
func shouldUseResponsesAPI(model string) bool {
	return strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4")
}

func staticModelsToAgent(staticModels []config.StaticModel) []agent.ModelInfo {
	models := make([]agent.ModelInfo, len(staticModels))
	for i, sm := range staticModels {
		models[i] = agent.ModelInfo{
			ID:               sm.ID,
			DisplayName:      sm.DisplayName,
			MaxContextWindow: sm.MaxContextWindow,
			ConfigName:       sm.ConfigName,
		}
	}
	return models
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
		var models []agent.ModelInfo
		if len(pc.Models) > 0 {
			models = staticModelsToAgent(pc.Models)
		} else if discovered, err := codebase.DiscoverCocoPluginModels(); err == nil {
			models = discovered
			if model != "" && configName == "" {
				for _, m := range models {
					if m.ID == model || m.ConfigName == model {
						model = m.ID
						configName = m.ConfigName
						if pc.BaseURL == "" && m.BaseURL != "" {
							pc.BaseURL = m.BaseURL
						}
						break
					}
				}
			}
		}
		c := codebase.NewClient(pc.APIKey, model, configName, pc.BaseURL)
		if len(models) > 0 {
			c.SetModels(models)
		}
		authHelper := pc.AuthHelper
		if authHelper == "" && pc.APIKey == "" {
			authHelper = codebase.DefaultAuthHelper
		}
		if authHelper != "" {
			c.SetAuthHelper(authHelper)
		}
		return c
	case "aiden":
		c := aiden.NewClient(pc.APIKey, model, pc.BaseURL)
		if pc.AuthHelper != "" {
			c.SetAuthHelper(pc.AuthHelper)
		}
		if shouldUseResponsesAPI(model) {
			c.SetUseResponsesAPI(true)
		}
		return c
	case "bytedance":
		c := aiden.NewClient(pc.APIKey, model, pc.BaseURL)
		if pc.AuthHelper != "" {
			c.SetAuthHelper(pc.AuthHelper)
		}
		if shouldUseResponsesAPI(model) {
			c.SetUseResponsesAPI(true)
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
		case "version", "--version", "-v":
			fmt.Println("cece", version.Version)
			os.Exit(0)
		case "update":
			os.Exit(runUpdate())
		case "setup":
			os.Exit(runSetup())
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
  cece engine       Run engine process (internal)
  cece setup        Interactive setup wizard
  cece update       Check for updates and install the latest version
  cece version      Print version

Commands:
  help              Show this help
  setup             Configure cece for first use
  version           Print version and exit
  update            Self-update to the latest release
`)
}

func runUpdate() int {
	ctx := context.Background()
	current := version.Version

	fmt.Printf("cece %s — checking for updates...\n", current)

	info, err := update.Check(ctx, current)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}

	if !info.Available() {
		fmt.Printf("Already up to date (v%s).\n", info.Current)
		return 0
	}

	fmt.Printf("New version available: v%s → v%s\n", info.Current, info.Latest)
	fmt.Printf("Downloading...\n")

	newVersion, err := update.SelfUpdate(ctx, info)
	if err != nil {
		fmt.Fprintf(os.Stderr, "self-update failed: %v\n", err)
		return 1
	}

	fmt.Printf("Updated to v%s. Restart cece to use the new version.\n", newVersion)
	return 0
}

func runSetup() int {
	projectDir, _ := os.Getwd()
	model := setup.NewSetupModel(projectDir)
	program := tea.NewProgram(&model)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
		return 1
	}
	return 0
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

	model := ui.NewModel(client, meta.model, projectDir)
	model.SetDefaultEffort(meta.defaultEffort)
	model.SetDefaultMode(meta.defaultMode)
	model.SetSessions(session.NewFileStore(projectDir))
	skillStore := skill.NewStore(skill.DiscoverAll(projectDir))
	skillStore.SetEnabled(meta.enabledSkills)
	model.SetSkillStore(skillStore)

	program := tea.NewProgram(&model)
	if _, err := program.Run(); err != nil {
		logger.Error("program exited", "error", err)
		return 1
	}
	return 0
}

func runEngineStdio(defaultProjectDir string, args []string) int {
	var projectDir string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project-dir":
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++
			}
		case "--stdio":
			// default mode; flag retained for backward compatibility
		}
	}

	bundle, err := buildRuntime(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine init failed: %v\n", err)
		return 1
	}
	host := runtime.NewHost(&runtime.Bundle{
		Engine:   bundle.mediator.Engine,
		Mediator: bundle.mediator,
		Store:    bundle.store,
		Skills:   bundle.skillStore,
		Cleanup:  bundle.cleanup,
	})
	defer host.Close()

	ctx := context.Background()
	if err := ipc.Serve(ctx, host, os.Stdin, os.Stdout); err != nil {
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
	logDir := filepath.Join(projectDir, ".cece", "log")
	if err := logger.Init(logDir, cfg.Debug); err != nil {
		return runtimeMetadata{}, fmt.Errorf("logger init failed: %w", err)
	}
	logger.Info("cece tui starting", "model", cfg.Model)
	return runtimeMetadata{
		model:         cfg.Model,
		contextWindow: cfg.ContextWindowFor(cfg.Model),
		defaultMode:   cfg.DefaultMode,
		defaultEffort: cfg.Effort,
		debug:         cfg.Debug,
		enabledSkills: cfg.EnabledSkills,
	}, nil
}

func buildRuntime(projectDir string) (runtimeBundle, error) {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return runtimeBundle{}, fmt.Errorf("config load failed: %w", err)
	}

	logDir := filepath.Join(projectDir, ".cece", "log")
	if err := logger.Init(logDir, cfg.Debug); err != nil {
		return runtimeBundle{}, fmt.Errorf("logger init failed: %w", err)
	}

	defaultProvider := selectDefaultProvider(cfg)
	logger.Info("cece engine starting", "model", cfg.Model, "provider", defaultProvider.Name, "maxTokens", cfg.MaxTokens)

	client := createClient(defaultProvider, cfg.Model, "")

	ctx := context.Background()
	contextWindow := discoverContextWindow(ctx, client, cfg)

	mcpMgr := mcp.NewManager()
	if len(cfg.MCP) > 0 {
		mcpMgr.Initialize(ctx, cfg.MCP)
	}

	skillStore := skill.NewStore(skill.DiscoverAll(projectDir))
	skillStore.SetEnabled(cfg.EnabledSkills)
	store := session.NewFileStore(projectDir)

	createClientFn := func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient {
		pc := config.ProviderConfig{
			Protocol: protocol, APIKey: apiKey, BaseURL: baseURL,
			AuthMode: authMode, AuthHelper: authHelper,
		}
		return createClient(pc, model, configName)
	}
	providerResolver := func(configName string) (string, string, string, string, string) {
		for _, p := range cfg.Providers {
			if configName != "" {
				if p.Protocol == "codebase" {
					if models, err := codebase.DiscoverCocoPluginModels(); err == nil {
						for _, m := range models {
							if m.ConfigName == configName || m.ID == configName {
								baseURL := p.BaseURL
								if m.BaseURL != "" {
									baseURL = m.BaseURL
								}
								authHelper := p.AuthHelper
								if authHelper == "" && p.APIKey == "" {
									authHelper = codebase.DefaultAuthHelper
								}
								return p.APIKey, baseURL, p.AuthMode, authHelper, p.Protocol
							}
						}
					}
				}
				for _, m := range p.Models {
					if m.ConfigName == configName {
						return p.APIKey, p.BaseURL, p.AuthMode, p.AuthHelper, p.Protocol
					}
				}
			}
			if p.Name == configName {
				authHelper := p.AuthHelper
				if p.Protocol == "codebase" && authHelper == "" && p.APIKey == "" {
					authHelper = codebase.DefaultAuthHelper
				}
				return p.APIKey, p.BaseURL, p.AuthMode, authHelper, p.Protocol
			}
		}
		if len(cfg.Providers) > 0 {
			p := defaultProvider
			authHelper := p.AuthHelper
			if p.Protocol == "codebase" && authHelper == "" && p.APIKey == "" {
				authHelper = codebase.DefaultAuthHelper
			}
			return p.APIKey, p.BaseURL, p.AuthMode, authHelper, p.Protocol
		}
		return "", "", "", "", ""
	}
	listAllModelsFn := buildListAllModelsFn(cfg)
	modelClientFor := func(model string) agent.ModelClient {
		for _, p := range cfg.Providers {
			for _, m := range p.Models {
				if m.ID == model || m.ConfigName == model {
					return createClient(p, model, m.ConfigName)
				}
			}
			if p.Protocol == "codebase" {
				if models, err := codebase.DiscoverCocoPluginModels(); err == nil {
					for _, m := range models {
						if m.ID == model || m.ConfigName == model {
							pc := p
							if m.BaseURL != "" {
								pc.BaseURL = m.BaseURL
							}
							return createClient(pc, m.ID, m.ConfigName)
						}
					}
				}
			}
		}
		if len(cfg.Providers) > 0 {
			return createClient(defaultProvider, model, "")
		}
		return nil
	}
	var lightClientFn runtime.LightModelClientFn
	if cfg.LightModel != "" {
		lightModel := cfg.LightModel
		lightClientFn = func() agent.ModelClient {
			return modelClientFor(lightModel)
		}
	}

	bundle, err := runtime.Build(runtime.Options{
		ProjectDir:             projectDir,
		Model:                  cfg.Model,
		ContextWindow:          contextWindow,
		MaxTokens:              cfg.MaxTokens,
		Yolo:                   cfg.Yolo,
		DefaultMode:            cfg.DefaultMode,
		DefaultEffort:          cfg.Effort,
		LintConfig:             cfg.Lint,
		PlanModeWriteAllowlist: cfg.PlanModeWriteAllowlist,
		ModelClient:            client,
		Store:                  store,
		Skills:                 skillStore,
		MCPManager:             mcpMgr,
		ProviderResolver:       providerResolver,
		CreateClientFn:         createClientFn,
		ListAllModelsFn:        listAllModelsFn,
		ContextWindowFor:       cfg.ContextWindowFor,
		ModelClientFor:         modelClientFor,
		LightClientFn:          lightClientFn,
	})
	if err != nil {
		return runtimeBundle{}, err
	}

	// Emit EngineReadyEvent so the TUI can sync contextWindow immediately.
	bundle.Engine.EmitEvent(protocol.EngineReadyEvent{
		Model:         cfg.Model,
		ContextWindow: contextWindow,
		Effort:        cfg.Effort,
	})

	return runtimeBundle{
		mediator:      bundle.Mediator,
		store:         bundle.Store,
		skillStore:    bundle.Skills,
		model:         cfg.Model,
		contextWindow: contextWindow,
		defaultMode:   cfg.DefaultMode,
		defaultEffort: cfg.Effort,
		cleanup:       bundle.Cleanup,
	}, nil
}

func selectDefaultProvider(cfg config.Config) config.ProviderConfig {
	if cfg.DefaultProvider != "" {
		for _, p := range cfg.Providers {
			if p.Name == cfg.DefaultProvider {
				return p
			}
		}
	}
	return cfg.Providers[0]
}

// discoverContextWindow queries the model client for its context window,
// falling back to ListModels then config defaults.
func discoverContextWindow(ctx context.Context, client agent.ModelClient, cfg config.Config) int {
	if lister, ok := client.(interface {
		GetModelInfo(context.Context) (agent.ModelInfo, error)
	}); ok {
		if info, err := lister.GetModelInfo(ctx); err == nil {
			logger.Info("model context window set from API", "max_context", info.MaxContextWindow)
			return info.MaxContextWindow
		} else {
			logger.Warn("model info query failed, trying ListModels", "error", err)
		}
	}
	if lister, ok := client.(interface {
		ListModels(context.Context) ([]agent.ModelInfo, error)
	}); ok {
		if models, err := lister.ListModels(ctx); err == nil {
			for _, m := range models {
				if m.ID == cfg.Model || m.ConfigName == cfg.Model {
					logger.Info("model context window set from ListModels", "max_context", m.MaxContextWindow)
					return m.MaxContextWindow
				}
			}
		} else {
			logger.Warn("ListModels failed, using config/default", "error", err)
		}
	}
	cw := cfg.ContextWindowFor(cfg.Model)
	logger.Info("model context window from config", "max_context", cw)
	return cw
}

// buildListAllModelsFn returns a closure that aggregates models across all providers.
func buildListAllModelsFn(cfg config.Config) runtime.ListAllModelsFn {
	return func(ctx context.Context) ([]protocol.ModelInfo, error) {
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
					models = staticModelsToAgent(pc.Models)
				}

				if len(models) == 0 {
					return
				}

				dtoModels := make([]protocol.ModelInfo, len(models))
				for i, m := range models {
					baseURL := pc.BaseURL
					if m.BaseURL != "" {
						baseURL = m.BaseURL
					}
					authHelper := pc.AuthHelper
					if pc.Protocol == "codebase" && authHelper == "" && pc.APIKey == "" {
						authHelper = codebase.DefaultAuthHelper
					}
					dtoModels[i] = protocol.ModelInfo{
						ID:               m.ID,
						DisplayName:      m.DisplayName,
						MaxContextWindow: m.MaxContextWindow,
						Provider:         pc.Name,
						APIKey:           pc.APIKey,
						BaseURL:          baseURL,
						AuthMode:         pc.AuthMode,
						AuthHelper:       authHelper,
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
}
