package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"cece/internal/prompt"
	"cece/internal/tool"
)

const escalatedMaxTokens = 64000

type Runtime struct {
	mu              sync.Mutex
	client          ModelClient
	registry        *tool.Registry
	assembler       *prompt.ContextAssembler
	projectDir      string
	history         []Message
	cancel          context.CancelFunc
	confirmCh       chan struct{} // set per Input call, cleared on completion
	yolo            bool          // auto-approve tool execution without UI confirmation
	maxTokens       int           // configurable max output tokens
	ContextWindowFor  func(model string) int // returns context window for a model ID
	listAllModelsFn   func(ctx context.Context) ([]ModelInfo, error)
	createClientFn    func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) ModelClient
}

func NewRuntime(client ModelClient, registry *tool.Registry, yolo bool, maxTokens int, assembler *prompt.ContextAssembler, projectDir string) *Runtime {
	return &Runtime{
		client:     client,
		registry:   registry,
		assembler:  assembler,
		projectDir: projectDir,
		yolo:       yolo,
		maxTokens:  maxTokens,
	}
}

func (r *Runtime) SetListAllModelsFn(fn func(ctx context.Context) ([]ModelInfo, error)) {
	r.listAllModelsFn = fn
}

func (r *Runtime) SetCreateClientFn(fn func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) ModelClient) {
	r.createClientFn = fn
}

func (r *Runtime) History() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Message, len(r.history))
	copy(out, r.history)
	return out
}

func (r *Runtime) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Confirm signals the Runtime to proceed with pending tool execution.
// Called by the UI after the user approves tool calls.
func (r *Runtime) Confirm() {
	r.mu.Lock()
	ch := r.confirmCh
	r.mu.Unlock()
	if ch != nil {
		ch <- struct{}{}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// modelResponse holds the result of a single callModel invocation.
type modelResponse struct {
	stopReason   string
	inputTokens  int
	outputTokens int
	toolCalls    []ApiToolUseBlock // non-empty when stopReason == "tool_use"
	textContent  string            // assistant text reply
}

// toolCallState tracks incremental assembly of a tool_use block across SSE events.
type toolCallState struct {
	id    string
	name  string
	input strings.Builder
}

// callModel executes one streaming API call, emits UI events to ch,
// and returns the parsed model response for the Agent loop.
func (r *Runtime) callModel(
	ctx context.Context,
	messages []Message,
	system SystemPrompt,
	ch chan<- Event,
	reason string,
	maxTokens int,
	toolResults []string,
) (modelResponse, error) {
	var tools []tool.Definition
	if r.registry != nil {
		tools = r.registry.Definitions()
	}

	estimated := estimateRequestTokens(system, messages, tools)
	ch <- UIModelRequestStarted{
		Reason:               reason,
		ToolResults:          toolResults,
		EstimatedInputTokens: estimated,
	}

	chunks, err := r.client.Stream(ctx, messages, system, tools, maxTokens)
	if err != nil {
		return modelResponse{}, err
	}

	start := time.Now()
	ch <- UIAssistantStarted{}

	var resp modelResponse
	var textBuf strings.Builder
	var thinkingBuf strings.Builder
	var thinkingIndex int = -1 // index of the current thinking block, -1 = none
	var toolInputStates map[int]*toolCallState

	for chunk := range chunks {
		if chunk.Err != nil {
			return modelResponse{}, chunk.Err
		}

		if chunk.EventType != "" && !chunk.Done {
			ch <- UIStreamEventDetail{
				EventType: chunk.EventType,
				Detail:    chunk.Detail,
				Text:      truncate(chunk.Delta, 60),
			}
		}

		if chunk.EventType == "message_start" {
			resp.inputTokens = chunk.InputTokens
			var toolNames []string
			for _, def := range tools {
				toolNames = append(toolNames, def.Name)
			}
			ch <- UIStreamStarted{
					InputTokens:         resp.inputTokens,
					Tools:               toolNames,
					CacheCreationTokens: chunk.CacheCreationTokens,
					CacheReadTokens:     chunk.CacheReadTokens,
				}
		}
		if chunk.EventType == "message_delta" {
			resp.outputTokens = chunk.OutputTokens
			resp.stopReason = chunk.StopReason
		}

		// Tool use assembly
		if chunk.EventType == "content_block_start" && chunk.ToolCallID != "" {
			if toolInputStates == nil {
				toolInputStates = make(map[int]*toolCallState)
			}
			toolInputStates[chunk.Index] = &toolCallState{
				id:   chunk.ToolCallID,
				name: chunk.ToolCallName,
			}
			ch <- UIToolCallStarted{
				ID:    chunk.ToolCallID,
				Name:  chunk.ToolCallName,
				Index: chunk.Index,
			}
		}
		if chunk.Detail == "input_json_delta" && chunk.ToolCallInput != "" {
			if ts, ok := toolInputStates[chunk.Index]; ok {
				ts.input.WriteString(chunk.ToolCallInput)
				ch <- UIToolCallDelta{
					ID:    ts.id,
					Index: chunk.Index,
					Input: chunk.ToolCallInput,
				}
			}
		}
		if chunk.EventType == "content_block_stop" {
			if ts, ok := toolInputStates[chunk.Index]; ok {
				raw := json.RawMessage(ts.input.String())
				resp.toolCalls = append(resp.toolCalls, ApiToolUseBlock{
					ID:    ts.id,
					Name:  ts.name,
					Input: raw,
				})
				ch <- UIToolCallCompleted{
					ID:    ts.id,
					Name:  ts.name,
					Input: raw,
					Index: chunk.Index,
				}
			}
		}

		// Thinking block assembly
		if chunk.EventType == "content_block_start" && chunk.IsThinking {
			thinkingIndex = chunk.Index
			thinkingBuf.Reset()
			ch <- UIThinkingStarted{Index: chunk.Index}
		}
		if chunk.Detail == "thinking_delta" && chunk.ThinkingDelta != "" {
			thinkingBuf.WriteString(chunk.ThinkingDelta)
			ch <- UIThinkingDelta{Text: chunk.ThinkingDelta}
		}
		if chunk.EventType == "content_block_stop" && thinkingIndex >= 0 && chunk.Index == thinkingIndex {
			fullThinking := thinkingBuf.String()
			thinkingIndex = -1
			thinkingBuf.Reset()
			ch <- UIThinkingCompleted{Text: fullThinking}
		}

		// Text delta (excludes thinking_delta which is routed above)
		if chunk.Delta != "" && chunk.Detail != "thinking_delta" {
			textBuf.WriteString(chunk.Delta)
			ch <- UIAssistantDelta{Text: chunk.Delta}
		}

		if chunk.Done {
			resp.textContent = textBuf.String()
			var callNames []string
			for _, tc := range resp.toolCalls {
				callNames = append(callNames, tc.Name)
			}
			ch <- UIStreamCompleted{
				OutputTokens: resp.outputTokens,
				StopReason:   resp.stopReason,
				Duration:     time.Since(start),
				ToolCalls:    callNames,
			}
			return resp, nil
		}
	}

	return modelResponse{}, errors.New("stream ended without message_stop")
}

// chanEmitter adapts an event channel to the tool.Emitter interface.
type chanEmitter struct {
	ch chan<- Event
	id string
}

func (e *chanEmitter) Emit(text string) {
	e.ch <- UIToolExecDelta{ID: e.id, Text: text}
}

// executeTools runs tool calls in parallel, emitting progress events to ch.
// Returns tool_result content blocks to be appended to the conversation.
func (r *Runtime) executeTools(ctx context.Context, calls []ApiToolUseBlock, ch chan<- Event) []ApiContentBlock {
	type execResult struct {
		index  int
		result tool.Result
	}

	results := make(chan execResult, len(calls))

	for i, call := range calls {
		go func(idx int, c ApiToolUseBlock) {
			emitter := &chanEmitter{ch: ch, id: c.ID}
			ch <- UIToolExecStarted{ID: c.ID, Name: c.Name}
			result := r.registry.Execute(ctx, c.Name, c.Input, emitter)
			ch <- UIToolExecCompleted{ID: c.ID, Name: c.Name, Result: result}
			results <- execResult{index: idx, result: result}
		}(i, call)
	}

	resultMap := make(map[int]tool.Result, len(calls))
	for range calls {
		r := <-results
		resultMap[r.index] = r.result
	}

	blocks := make([]ApiContentBlock, len(calls))
	for i, call := range calls {
		result := resultMap[i]
		blocks[i] = ApiContentBlock{
			Type: ApiToolResultContentType,
			ToolResult: &ApiToolResultBlock{
				ToolUseID: call.ID,
				Content:   result.Content,
				IsError:   result.IsError,
			},
		}
	}
	return blocks
}

func (r *Runtime) Input(ctx context.Context, input string) (<-chan Event, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("input must not be empty")
	}

	slog.Info("send called", "input", input)

	ctx, cancel := context.WithCancel(ctx)

	user := Message{Role: UserRole, Content: input}

	r.mu.Lock()
	r.cancel = cancel
	r.history = append(r.history, user)
	snapshot := make([]Message, len(r.history))
	copy(snapshot, r.history)
	r.mu.Unlock()

	events := make(chan Event)
	confirmCh := make(chan struct{}, 1)

	r.mu.Lock()
	r.confirmCh = confirmCh
	r.mu.Unlock()

	go func() {
		defer close(events)
		defer func() {
			r.mu.Lock()
			r.cancel = nil
			r.confirmCh = nil
			r.mu.Unlock()
		}()

		events <- UIUserMessageAdded{Message: user}

		// Assemble system prompt from three-layer context
		turnCtx := prompt.TurnContext{
			IncludeTime:             prompt.ShouldInjectTime(input),
			Now:                     time.Now(),
			CurrentWorkingDirectory: r.projectDir,
			Mode:                    "interactive",
			ConversationTurnNumber:  len(r.history)/2 + 1,
		}
		var systemPrompt SystemPrompt
		if r.assembler != nil {
			systemPrompt = AssembleResultToSystemPrompt(r.assembler.Assemble(turnCtx))
		}

		// Agent loop: keep calling the model until it stops requesting tools.
		messages := snapshot
		turnStart := time.Now()
		reason := "user"
		var toolResultNames []string
		for {
			resp, err := r.callModel(ctx, messages, systemPrompt, events, reason, r.maxTokens, toolResultNames)
			if err != nil {
				events <- UIRunFailed{Err: err}
				return
			}

			// Silent escalation: if output was truncated, retry once with 64K
			if resp.stopReason == "max_tokens" {
				events <- UITruncationRetry{
					Attempt:       1,
					PrevMaxTokens: r.maxTokens,
					NewMaxTokens:  escalatedMaxTokens,
				}
				slog.Info("output truncated, escalating max_tokens", "from", r.maxTokens, "to", escalatedMaxTokens)
				resp, err = r.callModel(ctx, messages, systemPrompt, events, reason, escalatedMaxTokens, toolResultNames)
				if err != nil {
					events <- UIRunFailed{Err: err}
					return
				}
			}

			// Build assistant message with content blocks
			var contentBlocks []ApiContentBlock
			if resp.textContent != "" {
				contentBlocks = append(contentBlocks, ApiContentBlock{
					Type: ApiTextContentType,
					Text: resp.textContent,
				})
			}
			for _, tc := range resp.toolCalls {
				contentBlocks = append(contentBlocks, ApiContentBlock{
					Type:    ApiToolUseContentType,
					ToolUse: &tc,
				})
			}

			assistant := Message{
				Role:          AssistantRole,
				Content:       resp.textContent,
				ContentBlocks: contentBlocks,
			}
			r.mu.Lock()
			r.history = append(r.history, assistant)
			r.mu.Unlock()

			// No tool calls -- conversation turn is done
			if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
				events <- UIAssistantCompleted{Duration: time.Since(turnStart)}
				return
			}

			// Tool calls requested
			if r.yolo {
				// Yolo mode: skip UI confirmation, execute immediately
			} else {
				events <- UIToolCallsReady{Calls: resp.toolCalls}
				select {
				case <-confirmCh:
				case <-ctx.Done():
					events <- UIRunFailed{Err: ctx.Err()}
					return
				}
			}

			// Execute tools
			toolResults := r.executeTools(ctx, resp.toolCalls, events)
			toolResultNames = make([]string, len(resp.toolCalls))
			for i, tc := range resp.toolCalls {
				toolResultNames[i] = tc.Name
			}

			// Append tool_result as a user message
			resultMsg := Message{
				Role:          UserRole,
				ContentBlocks: toolResults,
			}
			r.mu.Lock()
			r.history = append(r.history, resultMsg)
			messages = make([]Message, len(r.history))
			copy(messages, r.history)
			r.mu.Unlock()
			// Next callModel is triggered by tool results
			reason = "tool_result"
		}
	}()

	return events, nil
}

// modelSetter is implemented by clients that support runtime model switching.
type modelSetter interface {
	SetModel(model string)
	Model() string
}

// providerSetter is implemented by clients that support switching API provider.
type providerSetter interface {
	SetProvider(apiKey, baseURL string, authMode int)
}

// authHelperSetter is implemented by clients that support dynamic token fetching.
type authHelperSetter interface {
	SetAuthHelper(helper string)
}

// SwitchModel updates the active model, provider, and context window size.
// If maxContextWindow <= 0, falls back to ContextWindowFor then 200K.
// When createClientFn is set, the client is replaced entirely on switch
// so that cross-protocol transitions (anthropic ↔ openai) work correctly.
func (r *Runtime) SwitchModel(model string, maxContextWindow int, apiKey string, baseURL string, authMode string, authHelper string, protocol string, configName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createClientFn != nil {
		// Full client replacement — handles cross-protocol switching
		r.client = r.createClientFn(protocol, apiKey, model, baseURL, authMode, authHelper, configName)
		slog.Info("client replaced on model switch", "protocol", protocol, "model", model)
	} else {
		// Legacy path: reconfigure existing client (same protocol only)
		if ms, ok := r.client.(modelSetter); ok {
			ms.SetModel(model)
		}
		if ps, ok := r.client.(providerSetter); ok {
			ps.SetProvider(apiKey, baseURL, parseAuthMode(authMode))
		}
		if ahs, ok := r.client.(authHelperSetter); ok && authHelper != "" {
			ahs.SetAuthHelper(authHelper)
		}
	}

	if maxContextWindow <= 0 {
		if r.ContextWindowFor != nil {
			maxContextWindow = r.ContextWindowFor(model)
		}
		if maxContextWindow <= 0 {
			maxContextWindow = 200000
		}
	}
	r.assembler.SetMaxContextTokens(maxContextWindow)
	slog.Info("model switched", "model", model, "max_context", maxContextWindow)
}

// ListAllModels returns available models from all configured providers.
func (r *Runtime) ListAllModels(ctx context.Context) ([]ModelInfo, error) {
	if r.listAllModelsFn == nil {
		return nil, errors.New("multi-provider listing not configured")
	}
	return r.listAllModelsFn(ctx)
}

// parseAuthMode converts a string auth mode to an int matching claude.AuthMode values.
// This avoids a circular import of the claude package.
func parseAuthMode(s string) int {
	switch strings.ToLower(s) {
	case "bearer":
		return 1 // claude.AuthModeBearer
	default:
		return 0 // claude.AuthModeAPIKey
	}
}
