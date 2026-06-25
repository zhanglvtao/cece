package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/ui/theme"
)

type blockKind string

const (
	blockUser      blockKind = "user"
	blockAssistant blockKind = "assistant"
	blockThinking  blockKind = "thinking"
	blockTool      blockKind = "tool"
	blockSystem    blockKind = "system"
	blockError     blockKind = "error"
	blockPlan      blockKind = "plan"
	blockInfo      blockKind = "info"
	blockView      blockKind = "view"
)

type transcriptBlock struct {
	kind       blockKind
	title      string
	text       string
	done       bool
	err        bool
	quietOk    bool   // quiet tool completed successfully — render inline ✓
	toolName   string // set for blockTool, used for quiet-tool suppression
	toolParams string // set for blockTool, parameter text rendered after [Name] without highlight
	toolMeta   string // set for blockTool, request metadata rendered on the same line

	// Bash-specific: execution timing
	execStartedAt time.Time // when ToolExecStarted was received (Bash only)
	execDuration  time.Duration

	// Thinking block timing
	startedAt time.Time     // when ThinkingStarted was received
	duration  time.Duration // elapsed time when ThinkingCompleted

	// Incremental rendering: dirty=true means this block needs re-render.
	dirty        bool
	cachedRender string // cached output of renderBlock
	cachedWidth  int    // width that produced cachedRender
}

type transcript struct {
	blocks              []transcriptBlock
	currentAssistant    int
	currentThinking     int
	toolByID            map[string]int
	inputTokens         int
	outputTokens        int
	contextUsed         int
	lastStopReason      string
	cacheReadTokens     int
	cacheCreationTokens int

	// Incremental rendering cache
	cachedFullRender  string
	cachedRenderWidth int
	streamingMD       streamingMarkdown
}

func newTranscript() transcript {
	return transcript{
		currentAssistant: -1,
		currentThinking:  -1,
		toolByID:         make(map[string]int),
	}
}

func (t *transcript) reset() {
	// Preserve cumulative token statistics across clears.
	inputTok := t.inputTokens
	outputTok := t.outputTokens
	cacheRead := t.cacheReadTokens
	cacheCreation := t.cacheCreationTokens
	*t = newTranscript()
	t.inputTokens = inputTok
	t.outputTokens = outputTok
	t.cacheReadTokens = cacheRead
	t.cacheCreationTokens = cacheCreation
	t.streamingMD.Reset()
}

// markDirty marks a block as needing re-render and invalidates the full render cache.
func (t *transcript) markDirty(idx int) {
	if idx >= 0 && idx < len(t.blocks) {
		t.blocks[idx].dirty = true
	}
	t.cachedFullRender = ""
}

// invalidateAllCaches marks every block dirty and clears the full render cache.
func (t *transcript) invalidateAllCaches() {
	for i := range t.blocks {
		t.blocks[i].dirty = true
		t.blocks[i].cachedRender = ""
		t.blocks[i].cachedWidth = 0
	}
	t.cachedFullRender = ""
	t.streamingMD.Reset()
}

func (t *transcript) append(kind blockKind, title, text string) int {
	t.blocks = append(t.blocks, transcriptBlock{kind: kind, title: title, text: text, dirty: true})
	t.cachedFullRender = ""
	return len(t.blocks) - 1
}

func (t *transcript) appendDone(kind blockKind, title, text string) int {
	idx := t.append(kind, title, text)
	t.blocks[idx].done = true
	return idx
}

func (t *transcript) ensureAssistant() int {
	if t.currentAssistant >= 0 && t.currentAssistant < len(t.blocks) {
		return t.currentAssistant
	}
	t.currentAssistant = t.append(blockAssistant, "cece", "")
	return t.currentAssistant
}

func (t *transcript) ensureThinking() int {
	if t.currentThinking >= 0 && t.currentThinking < len(t.blocks) {
		return t.currentThinking
	}
	t.currentThinking = t.append(blockThinking, "thinking", "")
	return t.currentThinking
}

func formatModelRequestParts(estimatedInputTokens int, toolResults []string) []string {
	parts := []string{}
	if estimatedInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("estimated input: %d", estimatedInputTokens))
	}
	if len(toolResults) > 0 {
		parts = append(parts, "tool results: "+strings.Join(toolResults, ", "))
	}
	return parts
}

func (t *transcript) attachToolRequestMeta(toolResults []string, meta string) bool {
	if meta == "" {
		return false
	}
	idx := t.findToolBlockForResults(toolResults)
	if idx < 0 {
		return false
	}
	t.blocks[idx].toolMeta = meta
	t.markDirty(idx)
	return true
}

func (t *transcript) findToolBlockForResults(toolResults []string) int {
	want := map[string]struct{}{}
	for _, name := range toolResults {
		if name != "" {
			want[name] = struct{}{}
		}
	}
	if len(want) > 0 {
		for i := len(t.blocks) - 1; i >= 0; i-- {
			if t.blocks[i].kind != blockTool {
				continue
			}
			if _, ok := want[t.blocks[i].toolName]; ok {
				return i
			}
		}
	}
	for i := len(t.blocks) - 1; i >= 0; i-- {
		if t.blocks[i].kind == blockTool {
			return i
		}
	}
	return -1
}

// formatThinkingFooter returns a muted footer line for a completed thinking block
// showing the elapsed time, e.g. "Thought for 3s".
func formatThinkingFooter(block transcriptBlock) string {
	if !block.done || block.duration == 0 {
		return ""
	}
	d := block.duration
	var s string
	switch {
	case d < time.Second:
		s = "<1s"
	case d < time.Minute:
		s = fmt.Sprintf("%.0fs", d.Seconds())
	default:
		s = fmt.Sprintf("%.1fm", d.Minutes())
	}
	footerStyle := lipgloss.NewStyle().Foreground(theme.FgMuted).Faint(true)
	return footerStyle.Render("Thought for " + s)
}

func (t *transcript) apply(event protocol.Event) {
	switch e := event.(type) {
	case protocol.UserMessageAdded:
		t.currentAssistant = -1
		t.currentThinking = -1
		t.appendDone(blockUser, "you", e.Message.Content)
	case protocol.SystemReminderAdded:
		t.appendDone(blockSystem, "system", e.Content)
	case protocol.ModelRequestStarted:
		label := "request"
		if e.Reason != "" {
			label = e.Reason
		}
		parts := formatModelRequestParts(e.EstimatedInputTokens, e.ToolResults)
		if len(parts) > 0 {
			if e.Reason == "tool_result" && t.attachToolRequestMeta(e.ToolResults, strings.Join(parts, " | ")) {
				break
			}
			t.appendDone(blockInfo, label, strings.Join(parts, " | "))
		}
	case protocol.RequestDryRunEvent:
		t.contextUsed = e.EstimatedInputTokens
		t.appendDone(blockInfo, "dryrun", formatDryRun(e))
	case protocol.StreamStarted:
		if e.InputTokens > 0 {
			t.contextUsed = e.InputTokens
		}
		if e.CacheReadTokens > 0 || e.CacheCreationTokens > 0 {
			total := e.InputTokens
			if total == 0 {
				total = e.CacheReadTokens + e.CacheCreationTokens
			}
			hitRate := 0
			if total > 0 {
				hitRate = e.CacheReadTokens * 100 / total
			}
			var cacheParts []string
			if e.CacheCreationTokens > 0 {
				cacheParts = append(cacheParts, fmt.Sprintf("created:%s", formatTokenK(e.CacheCreationTokens)))
			}
			cacheParts = append(cacheParts, fmt.Sprintf("hit:%s", formatTokenK(e.CacheReadTokens)))
			cacheParts = append(cacheParts, fmt.Sprintf("input:%s", formatTokenK(total)))
			cacheParts = append(cacheParts, fmt.Sprintf("(%d%%)", hitRate))
			t.appendDone(blockInfo, "cache", strings.Join(cacheParts, " "))
		}
	case protocol.ThinkingStarted:
		idx := t.append(blockThinking, "thinking", "")
		t.blocks[idx].startedAt = time.Now()
		t.currentThinking = idx
	case protocol.ThinkingDelta:
		idx := t.ensureThinking()
		t.blocks[idx].text += e.Text
		t.markDirty(idx)
	case protocol.ThinkingCompleted:
		idx := t.ensureThinking()
		if e.Text != "" {
			t.blocks[idx].text = e.Text
		}
		if !t.blocks[idx].startedAt.IsZero() {
			t.blocks[idx].duration = time.Since(t.blocks[idx].startedAt)
		}
		t.blocks[idx].done = true
		t.markDirty(idx)
		t.currentThinking = -1
	case protocol.AssistantStarted:
		t.currentAssistant = t.append(blockAssistant, "cece", "")
	case protocol.AssistantDelta:
		idx := t.ensureAssistant()
		t.blocks[idx].text += e.Text
		t.markDirty(idx)
	case protocol.AssistantCompleted:
		if t.currentAssistant >= 0 && t.currentAssistant < len(t.blocks) {
			t.blocks[t.currentAssistant].done = true
			t.markDirty(t.currentAssistant)
			t.streamingMD.Reset()
		}
	case protocol.StreamCompleted:
		if e.InputTokens > 0 {
			t.inputTokens += e.InputTokens
			t.contextUsed = e.InputTokens
		}
		t.outputTokens += e.OutputTokens
		t.lastStopReason = e.StopReason
		if t.currentAssistant >= 0 && t.currentAssistant < len(t.blocks) {
			t.blocks[t.currentAssistant].done = true
			t.markDirty(t.currentAssistant)
			t.streamingMD.Reset()
		}
	case protocol.TruncationRetry:
		t.appendDone(blockInfo, "retry", fmt.Sprintf("output truncated, retrying with max_tokens %d -> %d", e.PrevMaxTokens, e.NewMaxTokens))
	case protocol.ToolCallStarted:
		idx := t.append(blockTool, "tool: "+e.Name, "")
		t.blocks[idx].toolName = e.Name
		t.toolByID[e.ID] = idx
	case protocol.ToolCallDelta:
		if idx, ok := t.toolByID[e.ID]; ok {
			t.blocks[idx].text += e.Delta
			t.markDirty(idx)
		}
	case protocol.ToolCallCompleted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		name, params := formatToolTitleKVs(e.Name, e.Input)
		if e.Name == "Edit" || e.Name == "Write" || e.Name == "Todo" {
			params = ""
		}
		t.blocks[idx].toolName = e.Name
		t.blocks[idx].title = name
		t.blocks[idx].toolParams = params
		if isQuietTool(e.Name) || e.Name == "Edit" || e.Name == "Write" {
			t.blocks[idx].text = ""
		} else {
			t.blocks[idx].text = formatToolPreview(e.Name, e.Input)
		}
		t.markDirty(idx)
	case protocol.ToolExecStarted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		t.blocks[idx].toolName = e.Name
		if isExecTool(e.Name) {
			// Bash: record start time, clear preview text for streaming output
			t.blocks[idx].execStartedAt = time.Now()
			t.blocks[idx].text = ""
		} else if isQuietTool(e.Name) {
			// Quiet tools: no streaming output displayed
		} else if e.Name != "Todo" {
			if t.blocks[idx].text == "" {
				t.blocks[idx].text = "running..."
			} else if !strings.Contains(t.blocks[idx].text, "\n---\n") {
				t.blocks[idx].text += "\n---\nrunning..."
			}
		}
		t.markDirty(idx)
	case protocol.ToolExecDelta:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool", "")
			t.toolByID[e.ID] = idx
		}
		if isQuietTool(t.blocks[idx].toolName) {
			break
		}
		if isExecTool(t.blocks[idx].toolName) {
			// Bash: append output directly, no "running..." management
			t.blocks[idx].text += e.Text
		} else {
			if strings.HasSuffix(t.blocks[idx].text, "running...") {
				t.blocks[idx].text = strings.TrimSuffix(t.blocks[idx].text, "running...")
			}
			t.blocks[idx].text += e.Text
		}
		t.markDirty(idx)
	case protocol.ToolExecCompleted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		t.blocks[idx].toolName = e.Name
		if isQuietTool(e.Name) {
			if e.Result.IsError {
				t.blocks[idx].text = "error: " + summarizeText(e.Result.Content, toolPreviewBytes, toolPreviewMaxLines)
				t.blocks[idx].err = true
			} else {
				t.blocks[idx].quietOk = true
				t.blocks[idx].text = ""
			}
			t.blocks[idx].done = true
			break
		}
		// Bash: native terminal-like output
		if isExecTool(e.Name) {
			result := tailLines(e.Result.Content, 20)
			// Compute duration
			if !t.blocks[idx].execStartedAt.IsZero() {
				t.blocks[idx].execDuration = time.Since(t.blocks[idx].execStartedAt)
			}
			if e.Result.IsError {
				t.blocks[idx].err = true
			}
			result = strings.TrimRight(result, "\n")
			status := formatBashStatus(e.Result.IsError, t.blocks[idx].execDuration)
			t.blocks[idx].text = result + "\n" + status
			t.blocks[idx].done = true
			t.markDirty(idx)
			break
		}
		if e.Name == "Todo" {
			if e.Result.IsError {
				t.blocks[idx].text = "error:\n" + summarizeText(e.Result.Content, toolPreviewBytes, toolPreviewMaxLines)
				t.blocks[idx].err = true
			} else {
				beforeOutput := strings.Split(t.blocks[idx].text, "\n---\n")[0]
				beforeOutput = strings.TrimSuffix(beforeOutput, "running...")
				t.blocks[idx].text = strings.TrimRight(beforeOutput, "\n")
			}
			t.blocks[idx].done = true
			t.markDirty(idx)
			break
		}
		var result string
		if isDiffTool(e.Name) || looksLikeDiff(e.Result.Content) {
			result = summarizeDiffPreview(e.Result.Content)
		} else {
			result = summarizeText(e.Result.Content, toolPreviewBytes, toolPreviewMaxLines)
		}
		prefix := "ok"
		if e.Result.IsError {
			prefix = "error"
			t.blocks[idx].err = true
		}
		beforeOutput := strings.Split(t.blocks[idx].text, "\n---\n")[0]
		beforeOutput = strings.TrimSuffix(beforeOutput, "running...")
		beforeOutput = strings.TrimRight(beforeOutput, "\n")
		if beforeOutput == "" || e.Name == "Edit" || e.Name == "Write" {
			t.blocks[idx].text = prefix + ":\n" + result
		} else {
			t.blocks[idx].text = beforeOutput + "\n---\n" + prefix + ":\n" + result
		}
		// Title was already set with KV params by ToolCallCompleted; skip here.
		t.blocks[idx].done = true
		t.markDirty(idx)
	case protocol.RunFailed:
		errMsg := "interrupted"
		if e.Err != "" && e.Err != "context canceled" {
			errMsg = e.Err
		}
		errMsg = appendErrorContext(errMsg)
		t.appendDone(blockError, "error", errMsg)
	case protocol.PlanApprovalRequested:
		t.appendDone(blockPlan, "plan: "+e.PlanFile, e.PlanContent)
	case protocol.PlanRejected:
		t.appendDone(blockInfo, "plan", "Plan rejected — staying in plan mode")
	case protocol.ToolCallsRejected:
		t.appendDone(blockInfo, "rejected", "Tool calls rejected by user")
	case protocol.QuestionAsked:
		// Handled by modal; no transcript block needed.
	case protocol.SessionLoadedEvent:
		if e.Err == "" {
			t.reset()
			t.inputTokens = e.TotalInput
			t.outputTokens = e.TotalOutput
			t.contextUsed = e.LastInput
			t.loadHistory(e.History)
		}
	case protocol.ContextNudgedEvent:
		t.appendDone(blockInfo, "nudge", fmt.Sprintf("ctx %d%% used (%dK/%dK), %d turns since compact", e.ContextPct, (e.ContextUsed+999)/1000, (e.ContextWindow+999)/1000, e.TurnsSinceCompact))
	case protocol.TurnCompleted:
		// Use authoritative token data from the engine.
		if e.LastInputTokens > 0 {
			t.contextUsed = e.LastInputTokens
		}
		if e.TotalInputTokens > 0 {
			t.inputTokens = e.TotalInputTokens
		}
		if e.TotalOutputTokens > 0 {
			t.outputTokens = e.TotalOutputTokens
		}
		if e.CacheReadTokens > 0 || e.CacheCreationTokens > 0 {
			t.cacheReadTokens = e.CacheReadTokens
			t.cacheCreationTokens = e.CacheCreationTokens
		}
	case protocol.SessionTitleGeneratedEvent:
		if e.Err != "" {
			t.appendDone(blockInfo, "title", "title generation failed: "+e.Err)
		} else {
			t.appendDone(blockInfo, "title", e.Title)
		}
	}
}

func (t *transcript) loadHistory(messages []protocol.Message) {
	// Build tool_use_id -> name map from assistant messages so we can look up
	// tool names when processing tool_result blocks in user messages.
	toolNames := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, b := range msg.ContentBlocks {
			if b.Type == protocol.ToolUseContentType && b.ToolUse != nil {
				toolNames[b.ToolUse.ID] = b.ToolUse.Name
			}
		}
	}
	for _, msg := range messages {
		t.loadMessageWithNames(msg, toolNames)
	}
}

func (t *transcript) loadMessage(msg protocol.Message) {
	t.loadMessageWithNames(msg, nil)
}

func (t *transcript) loadMessageWithNames(msg protocol.Message, toolNames map[string]string) {
	switch msg.Role {
	case "user":
		if msg.Content != "" {
			t.appendDone(blockUser, "you", msg.Content)
			return
		}
		for _, b := range msg.ContentBlocks {
			if b.Type == protocol.ToolResultContentType && b.ToolResult != nil {
				name := ""
				if toolNames != nil {
					name = toolNames[b.ToolResult.ToolUseID]
				}
				if isQuietTool(name) {
					text := ""
					if b.ToolResult.IsError {
						text = "error: " + summarizeText(b.ToolResult.Content, toolPreviewBytes, toolPreviewMaxLines)
					}
					blk := t.appendDone(blockTool, "tool: "+name, text)
					t.blocks[blk].toolName = name
					if b.ToolResult.IsError {
						t.blocks[blk].err = true
					} else {
						t.blocks[blk].quietOk = true
					}
				} else if isExecTool(name) {
					result := tailLines(b.ToolResult.Content, 20)
					result = strings.TrimRight(result, "\n")
					status := formatBashStatus(b.ToolResult.IsError, 0)
					blk := t.appendDone(blockTool, "tool: "+name, result+"\n"+status)
					t.blocks[blk].toolName = name
					if b.ToolResult.IsError {
						t.blocks[blk].err = true
					}
				} else {
					content := b.ToolResult.Content
					if looksLikeDiff(content) {
						t.appendDone(blockTool, "tool result", summarizeDiffPreview(content))
					} else {
						t.appendDone(blockTool, "tool result", summarizeText(content, toolPreviewBytes, toolPreviewMaxLines))
					}
				}
			}
		}
	case "assistant":
		hasText := false
		for _, b := range msg.ContentBlocks {
			switch b.Type {
			case protocol.ThinkingContentType:
				if b.Text != "" {
					t.appendDone(blockThinking, "thinking", b.Text)
				}
			case protocol.TextContentType:
				hasText = true
				t.appendDone(blockAssistant, "cece", b.Text)
			case protocol.ToolUseContentType:
				if b.ToolUse != nil {
					name, params := formatToolTitleKVs(b.ToolUse.Name, b.ToolUse.Input)
					var preview string
					if b.ToolUse.Name == "Edit" || b.ToolUse.Name == "Write" {
						params = ""
					} else if b.ToolUse.Name == "Todo" {
						params = ""
						preview = formatToolPreview(b.ToolUse.Name, b.ToolUse.Input)
					} else if !isQuietTool(b.ToolUse.Name) {
						preview = formatToolPreview(b.ToolUse.Name, b.ToolUse.Input)
					}
					blk := t.appendDone(blockTool, name, preview)
					t.blocks[blk].toolName = b.ToolUse.Name
					t.blocks[blk].toolParams = params
				}
			}
		}
		if !hasText && msg.Content != "" {
			t.appendDone(blockAssistant, "cece", msg.Content)
		}
	}
}

func (t *transcript) render(width int, sty Styles) string {
	if width <= 0 {
		width = 80
	}

	// Fast path: no dirty blocks and same width -> return cached full render.
	if t.cachedFullRender != "" && t.cachedRenderWidth == width && !t.hasDirtyBlocks() {
		return t.cachedFullRender
	}

	// Width change invalidates all block caches.
	if t.cachedRenderWidth != width && t.cachedRenderWidth != 0 {
		for i := range t.blocks {
			t.blocks[i].dirty = true
			t.blocks[i].cachedRender = ""
			t.blocks[i].cachedWidth = 0
		}
	}

	renderOrder := t.renderOrderIndices()

	var b strings.Builder
	var prevKind blockKind
	for i, blockIdx := range renderOrder {
		block := &t.blocks[blockIdx]
		if i > 0 {
			b.WriteString(blockGap(prevKind, block.kind))
		}
		prevKind = block.kind
		if !block.dirty && block.cachedWidth == width && block.cachedRender != "" {
			b.WriteString(block.cachedRender)
		} else {
			rendered := t.renderBlockIncremental(*block, width, sty)
			block.cachedRender = rendered
			block.cachedWidth = width
			block.dirty = false
			b.WriteString(rendered)
		}
	}
	if len(renderOrder) == 0 {
		b.WriteString("Cece ready. Type a message and press Enter.")
	}

	result := b.String()
	t.cachedFullRender = result
	t.cachedRenderWidth = width
	return result
}

func (t *transcript) hasDirtyBlocks() bool {
	for i := range t.blocks {
		if t.blocks[i].dirty {
			return true
		}
	}
	return false
}

// blockGap returns the newline separator between two adjacent transcript blocks.
// Consecutive tool blocks are rendered tightly (single newline) so call chains
// are readable at a glance; all other semantic boundaries keep a blank line.
func blockGap(prev, next blockKind) string {
	if prev == blockTool && next == blockTool {
		return "\n"
	}
	return "\n\n"
}

func (t *transcript) renderOrderIndices() []int {
	var rest []int
	for i := range t.blocks {
		if t.blocks[i].kind == blockThinking && t.blocks[i].done && t.blocks[i].text == "" {
			// Skip collapsed thinking blocks with no content.
			continue
		}
		rest = append(rest, i)
	}
	return rest
}

func (t *transcript) renderBlockIncremental(block transcriptBlock, width int, sty Styles) string {
	if block.kind == blockAssistant && !block.done && block.text != "" {
		return t.renderStreamingAssistant(block, width, sty)
	}
	if block.kind == blockThinking && !block.done && block.text != "" {
		return t.renderStreamingThinking(block, width, sty)
	}
	return renderBlock(block, width, sty)
}

func (t *transcript) renderStreamingThinking(block transcriptBlock, width int, sty Styles) string {
	label := block.title
	if label == "" {
		label = string(block.kind)
	}
	label = formatTranscriptLabel(label + " ...")
	lbl := labelStyleForKind(block.kind, sty)
	text := strings.TrimRight(block.text, "\n")

	renderer := getThinkingMarkdownRenderer(width)
	if renderer == nil {
		return renderBlock(block, width, sty)
	}
	rendered := t.streamingMD.Render(text, width, renderer)

	return lbl.Render(label) + "\n" + indent(rendered, "  ")
}

func renderAssistantBody(rendered string, sty Styles) string {
	return sty.Chat.AssistantBody.Render(rendered)
}

func (t *transcript) renderStreamingAssistant(block transcriptBlock, width int, sty Styles) string {
	text := strings.TrimRight(block.text, "\n")

	renderer := getMarkdownRenderer(width)
	if renderer == nil {
		return renderBlock(block, width, sty)
	}
	rendered := t.streamingMD.Render(text, width, renderer)

	return renderAssistantBody(rendered, sty)
}

func (t *transcript) renderOrder() []transcriptBlock {
	var rest []transcriptBlock
	for i := range t.blocks {
		if t.blocks[i].kind == blockThinking && t.blocks[i].done && t.blocks[i].text == "" {
			continue
		}
		rest = append(rest, t.blocks[i])
	}
	return rest
}

func (t *transcript) lastPlanOffset(width int, sty Styles) (int, bool) {
	if width <= 0 {
		width = 80
	}
	offset := 0
	planOffset := 0
	found := false
	for i, block := range t.renderOrder() {
		if i > 0 {
			offset += 2
		}
		if block.kind == blockPlan {
			planOffset = offset
			found = true
		}
		offset += renderedHeight(renderBlock(block, width, sty))
	}
	return planOffset, found
}

func (t *transcript) lastPlanBlock() (transcriptBlock, bool) {
	for i := len(t.blocks) - 1; i >= 0; i-- {
		if t.blocks[i].kind == blockPlan {
			return t.blocks[i], true
		}
	}
	return transcriptBlock{}, false
}

func labelStyleForKind(kind blockKind, sty Styles) lipgloss.Style {
	switch kind {
	case blockUser:
		return sty.Chat.LabelUser
	case blockAssistant:
		return sty.Chat.LabelAssistant
	case blockThinking:
		return sty.Chat.LabelThinking
	case blockTool:
		return sty.Chat.LabelTool
	case blockError:
		return sty.Chat.LabelError
	case blockSystem:
		return sty.Chat.LabelSystem
	case blockPlan:
		return sty.Chat.LabelPlan
	case blockView:
		return sty.Chat.LabelView
	case blockInfo:
		return sty.Chat.LabelInfo
	default:
		return sty.Chat.LabelInfo
	}
}

func formatTranscriptLabel(label string) string {
	if label == "" {
		return label
	}
	runes := []rune(label)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] = runes[0] - 'a' + 'A'
	}
	return string(runes)
}

func renderBlock(block transcriptBlock, width int, sty Styles) string {
	label := string(block.kind)
	if block.title != "" {
		label = block.title
	}
	if !block.done && block.kind != blockUser && block.kind != blockSystem {
		label += " ..."
	}
	label = formatTranscriptLabel(label)
	// Truncate tool labels to fit one line.
	if block.kind == blockTool {
		maxLabel := width
		if maxLabel < 10 {
			maxLabel = 10
		}
		if len(label) > maxLabel {
			label = label[:maxLabel-3] + "..."
		}
		// Also truncate params/meta so the full line roughly fits within width.
		lineExtra := strings.TrimSpace(strings.Join([]string{block.toolParams, block.toolMeta}, "  "))
		if lineExtra != "" {
			maxExtra := width - len(label) - 1
			if maxExtra < 10 {
				maxExtra = 10
			}
			if len(lineExtra) > maxExtra {
				if block.toolParams != "" && block.toolMeta != "" {
					block.toolMeta = ""
					lineExtra = block.toolParams
				}
				if len(lineExtra) > maxExtra {
					lineExtra = lineExtra[:maxExtra-3] + "..."
				}
				block.toolParams = lineExtra
				block.toolMeta = ""
			}
		}
	}
	text := strings.TrimRight(block.text, "\n")
	if block.kind == blockUser {
		text = ansi.Wrap(text, max(20, width), "")
		return sty.Chat.UserBody.Render(text)
	}
	lbl := labelStyleForKind(block.kind, sty)
	// Thinking blocks: render as Markdown with subdued palette + optional footer.
	if block.kind == blockThinking {
		rendered := renderMarkdownThinking(text, width)
		footer := formatThinkingFooter(block)
		if footer != "" {
			rendered = rendered + "\n" + footer
		}
		return lbl.Render(label) + "\n" + indent(rendered, "  ")
	}
	// View blocks: render file content as markdown or syntax-highlighted code.
	if block.kind == blockView {
		rendered := renderViewContent(text, block.toolParams, width)
		return lbl.Render(label) + "\n" + indent(rendered, "  ")
	}
	// For tool blocks, render the name highlighted and params/meta plain.
	renderLabel := func() string {
		if block.kind != blockTool {
			return lbl.Render(label)
		}
		line := lbl.Render(label)
		if block.toolParams != "" {
			line += " " + block.toolParams
		}
		if block.toolMeta != "" {
			line += "  " + block.toolMeta
		}
		return line
	}
	if text == "" {
		if block.quietOk {
			check := lipgloss.NewStyle().Foreground(theme.Green).Render("✓")
			return renderLabel() + " " + check
		}
		return renderLabel()
	}
	// Markdown-rendered blocks: plan and completed assistant messages.
	if block.kind == blockPlan {
		rendered := renderMarkdown(text, width)
		return lbl.Render(label) + "\n" + rendered
	}
	if block.kind == blockAssistant && block.done {
		rendered := renderMarkdown(text, width)
		return renderAssistantBody(rendered, sty)
	}
	// Bash tools: no indent, no wrap — render like terminal output (may contain ANSI).
	if block.kind == blockTool && isExecTool(block.toolName) {
		return renderLabel() + "\n" + text
	}
	text = ansi.Wrap(text, max(20, width-4), "")
	// Diff coloring for tool blocks that contain unified diff output.
	if block.kind == blockTool {
		text = renderDiffText(text)
	}
	return renderLabel() + "\n" + indent(text, "  ")
}

func formatDryRun(e protocol.RequestDryRunEvent) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("input: %s\n", e.Input))
	b.WriteString(fmt.Sprintf("max_tokens: %d\n", e.MaxTokens))
	b.WriteString(fmt.Sprintf("estimated_input_tokens: %d\n", e.EstimatedInputTokens))
	b.WriteString("\n[prompt layers]\n")
	for _, layer := range e.PromptLayers {
		cache := "none"
		if len(layer.CacheControl) > 0 {
			cache = layer.CacheControl["type"]
		}
		b.WriteString(fmt.Sprintf("- %s tokens=%d cache=%s\n", layer.Name, layer.TokenEstimate, cache))
		b.WriteString(indent(strings.TrimRight(layer.Content, "\n"), "  "))
		b.WriteString("\n")
	}
	b.WriteString("\n[messages]\n")
	for _, msg := range e.Messages {
		b.WriteString(fmt.Sprintf("- #%d %s\n", msg.Index, msg.Role))
		b.WriteString(indent(strings.TrimRight(msg.Content, "\n"), "  "))
		b.WriteString("\n")
	}
	b.WriteString("\n[tools]\n")
	if len(e.Tools) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, tool := range e.Tools {
			b.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description))
			if props, ok := tool.InputSchema["properties"].(map[string]any); ok && len(props) > 0 {
				required := requiredFields(tool.InputSchema)
				for name, def := range props {
					prop, _ := def.(map[string]any)
					typ, _ := prop["type"].(string)
					desc, _ := prop["description"].(string)
					req := ""
					if containsString(required, name) {
						req = " [required]"
					}
					if desc != "" {
						b.WriteString(fmt.Sprintf("    %s (%s): %s%s\n", name, typ, desc, req))
					} else {
						b.WriteString(fmt.Sprintf("    %s (%s)%s\n", name, typ, req))
					}
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func requiredFields(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var fields []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				fields = append(fields, s)
			}
		}
		return fields
	}
	return nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// tailLines returns the last n lines of text. Used for exec tools like Bash.
func tailLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	total := len(lines)
	if total <= n {
		return text
	}
	start := total - n
	out := make([]string, 0, n)
	for i := start; i < total; i++ {
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

// appendErrorContext appends session ID and log file path to an error message
// so users can locate the corresponding logs.
func appendErrorContext(errMsg string) string {
	sid := logger.GetSessionID()
	lp := logger.LogPath()
	if sid == "" && lp == "" {
		return errMsg
	}
	var b strings.Builder
	b.WriteString(errMsg)
	b.WriteString("\n\n")
	if sid != "" {
		b.WriteString("session: ")
		b.WriteString(sid)
	}
	if lp != "" {
		if sid != "" {
			b.WriteString("  ")
		}
		b.WriteString("log: ")
		b.WriteString(lp)
	}
	return b.String()
}

// renderViewContent renders file content for the /view command.
// If lang is "markdown", returns glamour-rendered markdown directly.
// Otherwise wraps the content in a code fence with the given language
// so glamour's Chroma syntax highlighting produces colored output.
func renderViewContent(text string, lang string, width int) string {
	if text == "" {
		return ""
	}
	if lang == "markdown" {
		return renderMarkdown(text, width)
	}
	// Wrap in code fence for syntax highlighting via Chroma.
	fenced := "```" + lang + "\n" + text + "\n```"
	return renderMarkdown(fenced, width)
}
