package ui

import (
	"fmt"
	"strings"

	"cece/internal/protocol"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
)

type transcriptBlock struct {
	kind  blockKind
	title string
	text  string
	done  bool
	err   bool
}

type transcript struct {
	blocks           []transcriptBlock
	currentAssistant int
	currentThinking  int
	toolByID         map[string]int
	inputTokens      int
	outputTokens     int
	contextUsed      int
	lastStopReason   string
	cacheReadTokens  int
	cacheCreationTokens int
}

func newTranscript() transcript {
	return transcript{
		currentAssistant: -1,
		currentThinking:  -1,
		toolByID:         make(map[string]int),
	}
}

func (t *transcript) reset() {
	// Preserve token statistics across clears.
	inputTok := t.inputTokens
	outputTok := t.outputTokens
	cacheRead := t.cacheReadTokens
	cacheCreation := t.cacheCreationTokens
	ctxUsed := t.contextUsed
	*t = newTranscript()
	t.inputTokens = inputTok
	t.outputTokens = outputTok
	t.cacheReadTokens = cacheRead
	t.cacheCreationTokens = cacheCreation
	t.contextUsed = ctxUsed
}

func (t *transcript) append(kind blockKind, title, text string) int {
	t.blocks = append(t.blocks, transcriptBlock{kind: kind, title: title, text: text})
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
	t.currentAssistant = t.append(blockAssistant, "assistant", "")
	return t.currentAssistant
}

func (t *transcript) ensureThinking() int {
	if t.currentThinking >= 0 && t.currentThinking < len(t.blocks) {
		return t.currentThinking
	}
	t.currentThinking = t.append(blockThinking, "thinking", "")
	return t.currentThinking
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
		parts := []string{}
		if e.EstimatedInputTokens > 0 {
			parts = append(parts, fmt.Sprintf("estimated input: %d", e.EstimatedInputTokens))
			t.contextUsed = e.EstimatedInputTokens
		}
		if len(e.ToolResults) > 0 {
			parts = append(parts, "tool results: "+strings.Join(e.ToolResults, ", "))
		}
		if len(parts) > 0 {
			t.appendDone(blockInfo, label, strings.Join(parts, " | "))
		}
	case protocol.StreamStarted:
		if e.InputTokens > 0 {
			t.inputTokens += e.InputTokens
			t.contextUsed = e.InputTokens
		}
		t.cacheReadTokens += e.CacheReadTokens
		t.cacheCreationTokens += e.CacheCreationTokens
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
		t.currentThinking = t.append(blockThinking, "thinking", "")
	case protocol.ThinkingDelta:
		idx := t.ensureThinking()
		t.blocks[idx].text += e.Text
	case protocol.ThinkingCompleted:
		idx := t.ensureThinking()
		if e.Text != "" {
			t.blocks[idx].text = e.Text
		}
		t.blocks[idx].done = true
		t.currentThinking = -1
	case protocol.AssistantStarted:
		t.currentAssistant = t.append(blockAssistant, "assistant", "")
	case protocol.AssistantDelta:
		idx := t.ensureAssistant()
		t.blocks[idx].text += e.Text
	case protocol.AssistantCompleted:
		if t.currentAssistant >= 0 && t.currentAssistant < len(t.blocks) {
			t.blocks[t.currentAssistant].done = true
		}
	case protocol.StreamCompleted:
		t.outputTokens += e.OutputTokens
		t.lastStopReason = e.StopReason
		if t.currentAssistant >= 0 && t.currentAssistant < len(t.blocks) {
			t.blocks[t.currentAssistant].done = true
		}
	case protocol.TruncationRetry:
		t.appendDone(blockInfo, "retry", fmt.Sprintf("output truncated, retrying with max_tokens %d -> %d", e.PrevMaxTokens, e.NewMaxTokens))
	case protocol.ToolCallStarted:
		idx := t.append(blockTool, "tool: "+e.Name, "")
		t.toolByID[e.ID] = idx
	case protocol.ToolCallDelta:
		if idx, ok := t.toolByID[e.ID]; ok {
			t.blocks[idx].text += e.Delta
		}
	case protocol.ToolCallCompleted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		t.blocks[idx].title = "tool: " + e.Name
		t.blocks[idx].text = formatJSONPreview(e.Input)
	case protocol.ToolExecStarted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		if t.blocks[idx].text == "" {
			t.blocks[idx].text = "running..."
		} else if !strings.Contains(t.blocks[idx].text, "\n---\n") {
			t.blocks[idx].text += "\n---\nrunning..."
		}
	case protocol.ToolExecDelta:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool", "")
			t.toolByID[e.ID] = idx
		}
		if strings.HasSuffix(t.blocks[idx].text, "running...") {
			t.blocks[idx].text = strings.TrimSuffix(t.blocks[idx].text, "running...")
		}
		t.blocks[idx].text += e.Text
	case protocol.ToolExecCompleted:
		idx, ok := t.toolByID[e.ID]
		if !ok {
			idx = t.append(blockTool, "tool: "+e.Name, "")
			t.toolByID[e.ID] = idx
		}
		result := summarizeText(e.Result.Content, toolPreviewBytes, toolPreviewMaxLines)
		prefix := "ok"
		if e.Result.IsError {
			prefix = "error"
			t.blocks[idx].err = true
		}
		beforeOutput := strings.Split(t.blocks[idx].text, "\n---\n")[0]
		beforeOutput = strings.TrimSuffix(beforeOutput, "running...")
		beforeOutput = strings.TrimRight(beforeOutput, "\n")
		if beforeOutput == "" {
			t.blocks[idx].text = prefix + ":\n" + result
		} else {
			t.blocks[idx].text = beforeOutput + "\n---\n" + prefix + ":\n" + result
		}
		t.blocks[idx].title = "tool: " + e.Name
		t.blocks[idx].done = true
	case protocol.RunFailed:
		errMsg := "interrupted"
		if e.Err != "" && e.Err != "context canceled" {
			errMsg = e.Err
		}
		t.appendDone(blockError, "error", errMsg)
	case protocol.PlanApprovalRequested:
		t.appendDone(blockPlan, "plan: "+e.PlanFile, e.PlanContent)
	case protocol.QuestionAsked:
		// Handled by modal; no transcript block needed.
	case protocol.SessionLoadedEvent:
		if e.Err == "" {
			t.reset()
			t.inputTokens = e.TotalInput
			t.outputTokens = e.TotalOutput
			t.contextUsed = e.LastInput
			for _, msg := range e.History {
				t.loadMessage(msg)
			}
		}
	}
}

func (t *transcript) loadHistory(messages []protocol.Message) {
	for _, msg := range messages {
		t.loadMessage(msg)
	}
}

func (t *transcript) loadMessage(msg protocol.Message) {
	switch msg.Role {
	case "user":
		if msg.Content != "" {
			t.appendDone(blockUser, "you", msg.Content)
			return
		}
		for _, b := range msg.ContentBlocks {
			if b.Type == protocol.ToolResultContentType && b.ToolResult != nil {
				t.appendDone(blockTool, "tool result", summarizeText(b.ToolResult.Content, toolPreviewBytes, toolPreviewMaxLines))
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
				t.appendDone(blockAssistant, "assistant", b.Text)
			case protocol.ToolUseContentType:
				if b.ToolUse != nil {
					t.appendDone(blockTool, "tool: "+b.ToolUse.Name, formatJSONPreview(b.ToolUse.Input))
				}
			}
		}
		if !hasText && msg.Content != "" {
			t.appendDone(blockAssistant, "assistant", msg.Content)
		}
	}
}

func (t *transcript) render(width int, sty Styles) string {
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	for i, block := range t.blocks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderBlock(block, width, sty))
	}
	if len(t.blocks) == 0 {
		b.WriteString("Cece ready. Type a message and press Enter.")
	}
	return b.String()
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
	case blockInfo:
		return sty.Chat.LabelInfo
	default:
		return sty.Chat.LabelInfo
	}
}

func renderBlock(block transcriptBlock, width int, sty Styles) string {
	label := string(block.kind)
	if block.title != "" {
		label = block.title
	}
	if !block.done && block.kind != blockUser && block.kind != blockSystem {
		label += " ..."
	}
	text := strings.TrimRight(block.text, "\n")
	if block.kind == blockThinking {
		text = renderThinkingPreview(text)
	}
	lbl := labelStyleForKind(block.kind, sty)
	if text == "" {
		return lbl.Render("[" + label + "]")
	}
	// Markdown-rendered blocks: plan and completed assistant messages.
	if block.kind == blockPlan || (block.kind == blockAssistant && block.done) {
		rendered := renderMarkdown(text, width)
		return lbl.Render("["+label+"]") + "\n" + rendered
	}
	text = ansi.Wrap(text, max(20, width-4), "")
	return lbl.Render("["+label+"]") + "\n" + indent(text, "  ")
}

func renderThinkingPreview(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= 4 {
		return text
	}
	preview := append([]string{}, lines[:3]...)
	preview = append(preview, fmt.Sprintf("... %d lines hidden ...", len(lines)-4))
	return strings.Join(preview, "\n")
}
