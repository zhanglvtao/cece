package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cece/internal/chat"
	"cece/internal/ui/list"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ── User Message Item ───────────────────────────────────────────────────────

// userMessageItem renders a user chat message with background styling.
type userMessageItem struct {
	*list.Versioned
	styles  Styles
	content string
}

var _ list.Item = (*userMessageItem)(nil)

func (u *userMessageItem) Render(width int) string {
	return u.styles.Chat.UserMsg.Width(width).Render(u.content)
}

func (u *userMessageItem) Finished() bool { return true }

// ── Request Detail Item ─────────────────────────────────────────────────────

// requestDetailItem renders a one-line request summary below a user message.
type requestDetailItem struct {
	*list.Versioned
	styles      Styles
	inputTokens int
	tokensExact bool // true once the server returns precise usage; false while we only have a local estimate
	tools       []string
	toolResults []string
}

var _ list.Item = (*requestDetailItem)(nil)

func (r *requestDetailItem) Render(width int) string {
	label := r.styles.Chat.RequestLabel.Render("► Request")
	var parts []string
	parts = append(parts, label)
	if r.inputTokens > 0 {
		prefix := "in:"
		if !r.tokensExact {
			prefix = "in:~"
		}
		parts = append(parts, fmt.Sprintf("%s%s", prefix, formatTokenCount(r.inputTokens)))
	}
	if len(r.tools) > 0 {
		parts = append(parts, fmt.Sprintf("tools:%s", strings.Join(r.tools, "·")))
	}
	if len(r.toolResults) > 0 {
		parts = append(parts, fmt.Sprintf("results:%s", strings.Join(r.toolResults, "·")))
	}
	line := "  " + strings.Join(parts, "  ")
	return r.styles.Detail.Render(line)
}

func (r *requestDetailItem) Finished() bool { return true }

// ── Assistant Message Item ──────────────────────────────────────────────────

// assistantMessageItem renders an assistant chat message with ▎ prefix.
// While streaming, Finished() returns false so the list cache keeps re-rendering.
type assistantMessageItem struct {
	*list.Versioned
	styles      Styles
	content     strings.Builder
	finished    bool
	streamingMd streamingMarkdown
}

var _ list.Item = (*assistantMessageItem)(nil)

func (a *assistantMessageItem) Render(width int) string {
	const indent = "  "
	innerWidth := width - len(indent)
	if innerWidth < 1 {
		innerWidth = 1
	}
	text := a.content.String()
	renderer := markdownRenderer(innerWidth)
	mdRendered := a.streamingMd.Render(text, innerWidth, renderer)

	// Indent every line so the assistant block sits inset from the left edge.
	lines := strings.Split(mdRendered, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		b.WriteString(line)
	}
	return b.String()
}

func (a *assistantMessageItem) Finished() bool { return a.finished }

func (a *assistantMessageItem) AppendDelta(text string) {
	a.content.WriteString(text)
	a.Bump()
}

func (a *assistantMessageItem) Finish() {
	a.finished = true
	a.streamingMd.Reset()
	a.Bump()
}

// ── Tool Call Item ──────────────────────────────────────────────────────────

// toolCallItem renders a tool call with its execution status and output.
// It progresses through: assembling → executing → finished.
type toolCallItem struct {
	*list.Versioned
	styles     Styles
	id         string
	name       string
	args       strings.Builder // raw JSON fragments during streaming
	parsedArgs map[string]any  // set once UIToolCallCompleted arrives
	output     strings.Builder
	result     *toolResult
	finished   bool
}

type toolResult struct {
	content string
	isError bool
}

var _ list.Item = (*toolCallItem)(nil)

func (t *toolCallItem) Render(width int) string {
	var b strings.Builder
	if t.finished && t.result != nil {
		if t.result.isError {
			b.WriteString(t.styles.Chat.ToolCallErr.Render("✗ "))
		} else {
			b.WriteString(t.styles.Chat.ToolCallOk.Render("✓ "))
		}
	} else if t.output.Len() > 0 {
		b.WriteString(t.styles.Chat.ToolCallRun.Render("▶ "))
	} else {
		b.WriteString(t.styles.Chat.ToolCallRun.Render("▹ "))
	}

	b.WriteString(t.styles.Chat.ToolCallName.Render(t.name))

	// Show parsed args (human-readable) when available, otherwise raw JSON stream.
	if len(t.parsedArgs) > 0 {
		argPreview := formatToolArgs(t.name, t.parsedArgs)
		if len(argPreview) > 120 {
			argPreview = argPreview[:117] + "..."
		}
		if argPreview != "" {
			b.WriteString(" ")
			b.WriteString(t.styles.Chat.ToolCallArgs.Render(argPreview))
		}
	} else if t.args.Len() > 0 {
		argPreview := t.args.String()
		if len(argPreview) > 60 {
			argPreview = argPreview[:57] + "..."
		}
		b.WriteString(" ")
		b.WriteString(t.styles.Chat.ToolCallArgs.Render(argPreview))
	}

	if t.finished && t.result != nil {
		if t.result.isError {
			errPreview := t.result.content
			if len(errPreview) > 80 {
				errPreview = errPreview[:77] + "..."
			}
			b.WriteString("  " + t.styles.Chat.ToolCallErr.Render(errPreview))
		} else if t.name == "Edit" {
			// Render unified diff with colored Diff UI
			diffRendered := RenderDiff(t.result.content, t.styles.Chat.Diff, width-2)
			b.WriteString("\n  ")
			b.WriteString(strings.ReplaceAll(diffRendered, "\n", "\n  "))
		} else {
			lines := strings.Count(t.result.content, "\n") + 1
			b.WriteString(fmt.Sprintf("  %s", t.styles.Chat.ToolCallSummary.Render(fmt.Sprintf("(%d lines)", lines))))
		}
	} else if t.output.Len() > 0 {
		// Show last few lines of streaming output
		outputStr := t.output.String()
		outputLines := strings.Split(outputStr, "\n")
		showLines := outputLines
		if len(showLines) > 5 {
			showLines = showLines[len(showLines)-5:]
		}
		for _, line := range showLines {
			if line == "" {
				continue
			}
			b.WriteString("\n  ")
			b.WriteString(t.styles.Chat.ToolCallOutput.Render(line))
		}
	}

	return b.String()
}

func (t *toolCallItem) Finished() bool { return t.finished }

func (t *toolCallItem) AppendArgs(delta string) {
	t.args.WriteString(delta)
	t.Bump()
}

func (t *toolCallItem) AppendOutput(text string) {
	t.output.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		t.output.WriteByte('\n')
	}
	t.Bump()
}

func (t *toolCallItem) SetResult(content string, isError bool) {
	t.result = &toolResult{content: content, isError: isError}
	t.finished = true
	t.Bump()
}

// ── Detail Item ─────────────────────────────────────────────────────────────

// truncationRetryItem renders a one-line notice when output is truncated
// and the runtime is automatically retrying with a larger token limit.
type truncationRetryItem struct {
	*list.Versioned
	styles    Styles
	prev, now int
}

var _ list.Item = (*truncationRetryItem)(nil)

func (t *truncationRetryItem) Render(width int) string {
	text := fmt.Sprintf("Output truncated, retrying with %dK tokens...", t.now/1024)
	return t.styles.Chat.ToolCallRun.Width(width).Render("↻ " + text)
}

func (t *truncationRetryItem) Finished() bool { return true }

// loadingItem renders a braille spinner animation with label and elapsed timer
// while waiting for the LLM.
type loadingItem struct {
	*list.Versioned
	styles  Styles
	frame   int
	label   string
	startAt time.Time
}

var _ list.Item = (*loadingItem)(nil)

var brailleSpinner = []rune("⣾⣽⣻⢿⡿⣟⣯⣷")

func formatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%ds", mins, remainSecs)
}

func (l *loadingItem) Render(width int) string {
	ch := string(brailleSpinner[l.frame%len(brailleSpinner)])
	elapsed := time.Since(l.startAt).Truncate(100 * time.Millisecond)
	text := fmt.Sprintf("%s %s · %s", ch, l.label, formatDuration(elapsed))
	return l.styles.Chat.Assistant.Width(width).Render(text)
}

func (l *loadingItem) Finished() bool { return false }

func (l *loadingItem) Advance() {
	l.frame++
	l.Bump()
}

// interruptedItem renders an "Interrupted" indicator when the user cancels a request.
type interruptedItem struct {
	*list.Versioned
	styles  Styles
	message string
}

var _ list.Item = (*interruptedItem)(nil)

func (i *interruptedItem) Render(width int) string {
	return i.styles.Chat.ToolCallErr.Width(width).Render("✗ " + i.message)
}

func (i *interruptedItem) Finished() bool { return true }

// detailItem renders a DetailBlock below an assistant message.
// Pressing space/enter toggles the expanded state when focused.
type detailItem struct {
	*list.Versioned
	styles  Styles
	detail  *DetailBlock
	focused bool
}

var _ list.Item = (*detailItem)(nil)
var _ list.MouseClickable = (*detailItem)(nil)
var _ list.Focusable = (*detailItem)(nil)

func (d *detailItem) Render(width int) string {
	rendered := d.detail.Render(width, d.styles)
	if d.focused {
		// Add a visual indicator for the focused detail block
		lines := strings.Split(rendered, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				lines[i] = "▸ " + line
			}
		}
		rendered = strings.Join(lines, "\n")
	}
	return rendered
}

func (d *detailItem) Finished() bool { return true }

func (d *detailItem) SetFocused(focused bool) {
	if d.focused != focused {
		d.focused = focused
		d.Bump()
	}
}

func (d *detailItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn == ansi.MouseLeft {
		d.detail.Expanded = !d.detail.Expanded
		d.Bump()
		return true
	}
	return false
}

// ── Thinking Item ──────────────────────────────────────────────────────────

// thinkingItem renders a collapsible thinking block from extended thinking.
// Default expanded; the user can toggle via space/enter/mouse click.
type thinkingItem struct {
	*list.Versioned
	styles   Styles
	content  strings.Builder
	finished bool
	expanded bool
}

var _ list.Item = (*thinkingItem)(nil)
var _ list.MouseClickable = (*thinkingItem)(nil)

// maxThinkingPreviewLines is the auto-fold threshold for thinking content.
// When the thinking block exceeds this many lines, only the head/tail are
// shown, with a hidden-line summary in between.
const maxThinkingPreviewLines = 8

// estimateThinkingTokens approximates token count using a simple heuristic:
// ~4 chars per token for ASCII, ~1.5 chars per token for CJK.
// This avoids importing the prompt package and the tiktoken cost for a
// lightweight UI label.
func estimateThinkingTokens(s string) int {
	if s == "" {
		return 0
	}
	cjk := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			cjk++
		}
	}
	asciiBytes := len(s) - cjk*3
	if asciiBytes < 0 {
		asciiBytes = 0
	}
	return (asciiBytes+3)/4 + (cjk*2+2)/3
}

func (t *thinkingItem) Render(width int) string {
	const labelIndent = "  "
	const contentIndent = "    "
	tokens := estimateThinkingTokens(t.content.String())
	arrow := "▾"
	if !t.expanded {
		arrow = "▸"
	}
	label := t.styles.Chat.ThinkingLabel.Render(fmt.Sprintf("%s Thinking (%d tokens)", arrow, tokens))

	if !t.expanded || t.content.Len() == 0 {
		return labelIndent + label
	}

	rawLines := strings.Split(strings.TrimRight(t.content.String(), "\n"), "\n")
	var b strings.Builder
	b.WriteString(labelIndent)
	b.WriteString(label)

	contentStyle := t.styles.Chat.ThinkingContent
	noteStyle := t.styles.Chat.ThinkingLabel

	if len(rawLines) > maxThinkingPreviewLines {
		head := 5
		tail := 2
		hidden := len(rawLines) - head - tail
		writeLine := func(line string, style lipgloss.Style) {
			b.WriteByte('\n')
			b.WriteString(contentIndent)
			b.WriteString(style.Render(line))
		}
		for _, line := range rawLines[:head] {
			writeLine(line, contentStyle)
		}
		writeLine(fmt.Sprintf("… %d lines hidden …", hidden), noteStyle)
		for _, line := range rawLines[len(rawLines)-tail:] {
			writeLine(line, contentStyle)
		}
	} else {
		for _, line := range rawLines {
			b.WriteByte('\n')
			b.WriteString(contentIndent)
			b.WriteString(contentStyle.Render(line))
		}
	}
	return b.String()
}

func (t *thinkingItem) Finished() bool { return t.finished }

func (t *thinkingItem) AppendDelta(text string) {
	t.content.WriteString(text)
	t.Bump()
}

func (t *thinkingItem) Finish() {
	t.finished = true
	t.Bump()
}

func (t *thinkingItem) ToggleExpanded() {
	t.expanded = !t.expanded
	t.Bump()
}

func (t *thinkingItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn == ansi.MouseLeft {
		t.ToggleExpanded()
		return true
	}
	return false
}

// ── Chat Component ──────────────────────────────────────────────────────────

// Chat is the chat message list component. It wraps a list.List and manages
// message items (user, assistant, detail, tool call) as chat.Events arrive.
type Chat struct {
	list                *list.List
	styles              Styles
	currentAssistant    *assistantMessageItem
	currentThinking     *thinkingItem
	currentDetail       *detailItem
	currentRequest      *requestDetailItem
	loading             *loadingItem
	toolCalls           map[string]*toolCallItem // active tool calls by ID
	currentInputTokens  int                      // per-turn input tokens
	cacheCreationTokens int                      // from last UIStreamStarted
	cacheReadTokens     int                      // from last UIStreamStarted
	totalInputTokens    int                      // cumulative input tokens
	totalOutputTokens   int                      // cumulative output tokens
}

// NewChat creates a new Chat component.
func NewChat(styles Styles) *Chat {
	l := list.NewList()
	l.SetGap(1)
	l.RegisterRenderCallback(list.FocusedRenderCallback(l))
	return &Chat{
		list:   l,
		styles: styles,
	}
}

// Height returns the viewport height of the chat area.
func (c *Chat) Height() int {
	return c.list.Height()
}

// SetSize sets the viewport size for the chat area.
func (c *Chat) SetSize(width, height int) {
	c.list.SetSize(width, height)
}

// AtBottom returns whether the chat is scrolled to the bottom.
func (c *Chat) AtBottom() bool {
	return c.list.AtBottom()
}

// ScrollBy scrolls the chat by the given number of lines.
func (c *Chat) ScrollBy(lines int) {
	c.list.ScrollBy(lines)
}

// ScrollToBottom scrolls to the bottom of the chat.
func (c *Chat) ScrollToBottom() {
	c.list.ScrollToBottom()
}

// SetLoading adds a loading spinner to the chat.
func (c *Chat) SetLoading(item *loadingItem) {
	c.loading = item
	c.list.AppendItems(item)
	c.list.ScrollToBottom()
}

// RemoveLoading removes the loading spinner from the chat.
func (c *Chat) RemoveLoading() {
	if c.loading == nil {
		return
	}
	for i := 0; i < c.list.Len(); i++ {
		if c.list.ItemAt(i) == c.loading {
			c.list.RemoveItem(i)
			break
		}
	}
	c.loading = nil
}

// AdvanceLoading advances the spinner frame.
func (c *Chat) AdvanceLoading() {
	if c.loading != nil {
		c.loading.Advance()
	}
}

// ApplyEvent applies a chat.Event to update the chat state.
func (c *Chat) ApplyEvent(event chat.Event) {
	switch e := event.(type) {
	case chat.UIUserMessageAdded:
		c.currentAssistant = nil
		c.currentThinking = nil
		c.currentDetail = nil
		c.currentRequest = nil
		c.currentInputTokens = 0
		item := &userMessageItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			content:   e.Message.Content,
		}
		c.list.AppendItems(item)

		c.list.ScrollToBottom()

	case chat.UIModelRequestStarted:
		c.RemoveLoading()
		label := "Thinking"
		if e.Reason == "tool_result" {
			label = "Processing"
		}
		// Add request detail placeholder for this API call.
		// Pre-fill with the locally estimated input tokens so the user sees
		// "in:~N" immediately; UIStreamStarted will later overwrite with the
		// exact server-reported value.
		reqDetail := &requestDetailItem{
			Versioned:   list.NewVersioned(),
			styles:      c.styles,
			inputTokens: e.EstimatedInputTokens,
			tokensExact: false,
			toolResults: e.ToolResults,
		}
		c.currentRequest = reqDetail
		c.currentInputTokens = e.EstimatedInputTokens
		c.list.AppendItems(reqDetail)
		loading := &loadingItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			label:     label,
			startAt:   time.Now(),
		}
		c.SetLoading(loading)

	case chat.UIThinkingStarted:
		c.RemoveLoading()
		c.currentThinking = &thinkingItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			expanded:  true,
		}
		c.list.AppendItems(c.currentThinking)
		c.list.ScrollToBottom()

	case chat.UIThinkingDelta:
		if c.currentThinking == nil {
			c.currentThinking = &thinkingItem{
				Versioned: list.NewVersioned(),
				styles:    c.styles,
				expanded:  true,
			}
			c.list.AppendItems(c.currentThinking)
		}
		c.currentThinking.AppendDelta(e.Text)
		c.list.ScrollToBottom()

	case chat.UIThinkingCompleted:
		if c.currentThinking != nil {
			c.currentThinking.Finish()
		}

	case chat.UIAssistantStarted:
		if c.loading != nil {
			c.loading.label = "Generating"
			c.loading.Bump()
		}
		c.currentAssistant = &assistantMessageItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
		}
		c.list.AppendItems(c.currentAssistant)
		c.list.ScrollToBottom()

	case chat.UIAssistantDelta:
		if c.currentAssistant == nil {
			c.currentAssistant = &assistantMessageItem{
				Versioned: list.NewVersioned(),
				styles:    c.styles,
			}
			c.list.AppendItems(c.currentAssistant)
		}
		c.currentAssistant.AppendDelta(e.Text)
		c.list.ScrollToBottom()

	case chat.UIStreamStarted:
		c.cacheCreationTokens = e.CacheCreationTokens
		c.cacheReadTokens = e.CacheReadTokens
		// Backfill request detail. Only overwrite the input-token figure when
		// the server actually reported usage (some providers, notably OpenAI
		// without stream_options.include_usage, never send it); otherwise we
		// keep the local pre-flight estimate that was set in UIModelRequestStarted.
		if e.InputTokens > 0 {
			c.currentInputTokens = e.InputTokens
			c.totalInputTokens += e.InputTokens
			if c.currentRequest != nil {
				c.currentRequest.inputTokens = e.InputTokens
				c.currentRequest.tokensExact = true
			}
		}
		if c.currentRequest != nil {
			c.currentRequest.tools = e.Tools
			c.currentRequest.Bump()
		}

	case chat.UIStreamCompleted:
		c.RemoveLoading()
		c.totalOutputTokens += e.OutputTokens
		if c.currentAssistant != nil {
			c.currentAssistant.Finish()
		}
		detail := &DetailBlock{
			InputTokens:         c.currentInputTokens,
			OutputTokens:        e.OutputTokens,
			CacheCreationTokens: c.cacheCreationTokens,
			CacheReadTokens:     c.cacheReadTokens,
			Duration:            e.Duration,
			StopReason:          e.StopReason,
			ToolCalls:           e.ToolCalls,
		}
		c.currentDetail = &detailItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			detail:    detail,
		}
		c.list.AppendItems(c.currentDetail)
		c.list.ScrollToBottom()

	case chat.UITruncationRetry:
		c.list.AppendItems(&truncationRetryItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			prev:      e.PrevMaxTokens,
			now:       e.NewMaxTokens,
		})
		c.list.ScrollToBottom()

	case chat.UIAssistantCompleted:
		if c.currentAssistant != nil {
			c.currentAssistant.Finish()
		}

	case chat.UIRunFailed:
		c.RemoveLoading()
		if c.currentAssistant != nil {
			c.currentAssistant.Finish()
		}
		msg := "Interrupted"
		if e.Err != nil && e.Err.Error() != "context canceled" {
			msg = e.Err.Error()
		}
		c.list.AppendItems(&interruptedItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			message:   msg,
		})
		c.list.ScrollToBottom()

	case chat.UIToolCallStarted:
		if c.toolCalls == nil {
			c.toolCalls = make(map[string]*toolCallItem)
		}
		item := &toolCallItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			id:        e.ID,
			name:      e.Name,
		}
		c.toolCalls[e.ID] = item
		c.list.AppendItems(item)
		c.list.ScrollToBottom()

	case chat.UIToolCallDelta:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.AppendArgs(e.Input)
		}

	case chat.UIToolCallCompleted:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.AppendArgs(string(e.Input))
			// Parse the complete JSON input into human-readable args.
			var parsed map[string]any
			if json.Unmarshal(e.Input, &parsed) == nil {
				item.parsedArgs = parsed
			}
			item.Bump()
		}

	case chat.UIToolExecStarted:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.Bump()
		}
		c.list.ScrollToBottom()

	case chat.UIToolExecDelta:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.AppendOutput(e.Text)
		}
		c.list.ScrollToBottom()

	case chat.UIToolExecCompleted:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.SetResult(e.Result.Content, e.Result.IsError)
		}
		c.list.ScrollToBottom()
	}
}

// HandleMouseClick delegates mouse clicks to clickable items.
func (c *Chat) HandleMouseClick(x, y int) bool {
	idx, _ := c.list.ItemIndexAtPosition(x, y)
	if idx < 0 {
		return false
	}
	if item, ok := c.list.ItemAt(idx).(list.MouseClickable); ok {
		return item.HandleMouseClick(ansi.MouseLeft, x, y)
	}
	return false
}

// Draw renders the chat onto the screen.
func (c *Chat) Draw(scr uv.Screen, area uv.Rectangle) {
	content := c.list.Render()
	uv.NewStyledString(content).Draw(scr, area)
}

// Focus activates the chat list for keyboard navigation.
func (c *Chat) Focus() {
	c.list.Focus()
	if c.list.Selected() < 0 && c.list.Len() > 0 {
		c.list.SelectFirstInView()
	}
}

// Blur deactivates the chat list.
func (c *Chat) Blur() {
	c.list.Blur()
	c.list.SetSelected(-1)
}

// SelectPrev moves selection to the previous item.
func (c *Chat) SelectPrev() bool {
	return c.list.SelectPrev()
}

// SelectNext moves selection to the next item.
func (c *Chat) SelectNext() bool {
	return c.list.SelectNext()
}

// SelectFirstInView selects the first item currently visible in the viewport.
func (c *Chat) SelectFirstInView() {
	c.list.SelectFirstInView()
}

// SelectLastInView selects the last item currently visible in the viewport.
func (c *Chat) SelectLastInView() {
	c.list.SelectLastInView()
}

// ToggleExpand toggles the expanded state of the currently selected item
// if it is a detail block or thinking block.
func (c *Chat) ToggleExpand() {
	idx := c.list.Selected()
	if idx < 0 {
		return
	}
	item := c.list.ItemAt(idx)
	if d, ok := item.(*detailItem); ok {
		d.detail.Expanded = !d.detail.Expanded
		d.Bump()
	} else if t, ok := item.(*thinkingItem); ok {
		t.ToggleExpanded()
	}
}

// TokenInfo returns the cumulative input/output token counts.
func (c *Chat) TokenInfo() (input, output int) {
	return c.totalInputTokens, c.totalOutputTokens
}

// ContextUsed returns the current request input-token footprint used for the context gauge.
func (c *Chat) ContextUsed() int {
	return c.currentInputTokens
}
