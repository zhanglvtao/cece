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

	"cece/internal/aiden"
	"cece/internal/chat"
	"cece/internal/claude"
	"cece/internal/codebase"
	"cece/internal/config"
	"cece/internal/engine"
	"cece/internal/logger"
	"cece/internal/mcp"
	"cece/internal/prompt"
	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/skill"
	"cece/internal/tool"
	"cece/internal/ui"
)

func createClient(pc config.ProviderConfig, model string, configName string) chat.ModelClient {
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

	cfg, err := config.Load(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	logPath := filepath.Join(projectDir, ".cece", "cece.log")
	if err := logger.Init(logPath, cfg.Debug); err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	defaultProvider := cfg.Providers[0]
	logger.Info("cece starting", "model", cfg.Model, "provider", defaultProvider.Name, "maxTokens", cfg.MaxTokens)

	client := createClient(defaultProvider, cfg.Model, "")
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	// Initialize skill system
	skillStore := skill.NewStore(skill.DiscoverAll(projectDir))

	registry := tool.NewRegistry(
		tool.NewBash(),
		tool.NewRead(),
		tool.NewWrite(),
		tool.NewGrep(),
		tool.NewEdit(),
		tool.NewGlob(),
		tool.NewEnterPlanMode(planState),
		tool.NewExitPlanMode(planState),
		tool.NewAskUserQuestion(),
		tool.NewSkillTool(skillStore),
	)

	// Initialize session context and query model info for token budget
	ctx := context.Background()

	// Initialize MCP connections and register their tools
	mcpMgr := mcp.NewManager()
	if len(cfg.MCP) > 0 {
		mcpMgr.Initialize(ctx, cfg.MCP)
		for _, t := range mcpMgr.Tools() {
			registry.Register(t)
		}
	}
	defer mcpMgr.Close()

	stablePrompt := prompt.FormatStableSystemPrompt(projectDir)
	collector := prompt.NewDefaultSessionCollector(projectDir, registry)
	collector.SetSkillProvider(skillStore)
	assembler := prompt.NewContextAssembler(stablePrompt, registry, collector)

	if _, err := assembler.RefreshSession(ctx); err != nil {
		logger.Warn("initial session refresh failed", "error", err)
	}

	// Context window: GetModelInfo -> ListModels lookup -> config mapping -> 200K default
	var contextWindow int
	if lister, ok := client.(interface {
		GetModelInfo(context.Context) (chat.ModelInfo, error)
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
			ListModels(context.Context) ([]chat.ModelInfo, error)
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
	eng.SetModelInfo(cfg.Model, contextWindow)
	eng.SetToolResultPolicy(chat.ToolResultPolicy{
		InlineMaxLines: cfg.ToolResult.InlineMaxLines,
		HeadLines:      cfg.ToolResult.HeadLines,
		TailLines:      cfg.ToolResult.TailLines,
	})
	eng.ContextWindowFor = cfg.ContextWindowFor

	// Session persistence
	store := session.NewFileStore(projectDir)
	eng.SetStore(store)

	// Inject client factory for cross-protocol model switching
	createClientFn := func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) chat.ModelClient {
		pc := config.ProviderConfig{
			Protocol:   protocol,
			APIKey:     apiKey,
			BaseURL:    baseURL,
			AuthMode:   authMode,
			AuthHelper: authHelper,
		}
		return createClient(pc, model, configName)
	}

	// Inject provider resolver so session resume can rebuild clients with real credentials
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
		// Fallback: return first provider if no match
		if len(cfg.Providers) > 0 {
			p := cfg.Providers[0]
			return p.APIKey, p.BaseURL, p.AuthMode, p.AuthHelper, p.Protocol
		}
		return "", "", "", "", ""
	}

	// Inject multi-provider model listing
	listAllModelsFn := func(ctx context.Context) ([]protocol.ModelInfo, error) {
		var allModels []protocol.ModelInfo
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, p := range cfg.Providers {
			wg.Add(1)
			go func(pc config.ProviderConfig) {
				defer wg.Done()

				// Try API-based listing first
				var models []chat.ModelInfo
				tmpClient := createClient(pc, "", "")
				if lister, ok := tmpClient.(interface {
					ListModels(context.Context) ([]chat.ModelInfo, error)
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

				// Fallback to static model list from config
				if len(models) == 0 && len(pc.Models) > 0 {
					models = make([]chat.ModelInfo, len(pc.Models))
					for i, sm := range pc.Models {
						models[i] = chat.ModelInfo{
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

				// Convert internal models to dto
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

	mediator := engine.NewEngineMediator(eng, store, providerResolver, createClientFn, listAllModelsFn)
	model := ui.NewModel(mediator, cfg.Model, projectDir, contextWindow)
	model.SetSessions(store)
	model.SetSkillStore(skillStore)

	program := tea.NewProgram(&model)
	if _, err := program.Run(); err != nil {
		logger.Error("program exited", "error", err)
		os.Exit(1)
	}
}
