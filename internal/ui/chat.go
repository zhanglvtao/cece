package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cece/internal/protocol"
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
	request *requestDetailItem
}

var _ list.Item = (*userMessageItem)(nil)

func (u *userMessageItem) Render(width int) string {
	rendered := u.styles.Chat.UserMsgBg.Width(width).Render(u.styles.Chat.UserMsg.Render(u.content))
	if u.request != nil {
		rendered += "\n" + u.request.Render(width)
	}
	return rendered
}

func (u *userMessageItem) Finished() bool { return true }

type systemReminderItem struct {
	*list.Versioned
	styles  Styles
	content string
}

var _ list.Item = (*systemReminderItem)(nil)

func (s *systemReminderItem) Render(width int) string {
	return s.styles.Detail.Width(width).Render(s.content)
}

func (s *systemReminderItem) Finished() bool { return true }

// ── Queued Message Item ─────────────────────────────────────────────────────

// queuedMessageItem renders a user message that was queued while the assistant
// was busy. Shown with a muted style to indicate pending status.
type queuedMessageItem struct {
	*list.Versioned
	styles   Styles
	content  string
	promoted bool // set to true when this queued item is promoted
}

var _ list.Item = (*queuedMessageItem)(nil)

func (q *queuedMessageItem) Render(width int) string {
	if q.promoted {
		return q.styles.Chat.UserMsg.Width(width).Render(q.content)
	}
	return q.styles.Chat.UserMsg.
		Width(width).
		Faint(true).
		Render(q.content)
}

func (q *queuedMessageItem) Finished() bool { return true }

// Promote marks this queued item as promoted (removes faint styling).
func (q *queuedMessageItem) Promote() {
	q.promoted = true
	q.Bump()
}

// ── Request Detail Item ─────────────────────────────────────────────────────

// requestDetailItem renders a one-line request summary below a user message.
type requestDetailItem struct {
	*list.Versioned
	styles      Styles
	reason      string
	inputTokens int
	tokensExact bool // true once the server returns precise usage; false while we only have a local estimate
	tools       []string
	toolResults []string
}

var _ list.Item = (*requestDetailItem)(nil)

func (r *requestDetailItem) Render(width int) string {
	label := r.styles.Chat.RequestLabel.Render("req")
	parts := []string{label}
	if r.reason != "" {
		parts = append(parts, r.reason)
	}
	if r.inputTokens > 0 {
		count := formatTokenCount(r.inputTokens)
		if !r.tokensExact {
			count = "~" + count
		}
		parts = append(parts, count)
	}
	if preview := compactNameList(r.toolResults); preview != "" {
		parts = append(parts, preview)
	}
	if preview := compactNameList(r.tools); preview != "" {
		parts = append(parts, preview)
	}
	line := "  " + strings.Join(parts, " · ")
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
	response    *detailItem
}

var _ list.Item = (*assistantMessageItem)(nil)
var _ list.MouseClickable = (*assistantMessageItem)(nil)
var _ list.Focusable = (*assistantMessageItem)(nil)

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
	if a.response != nil {
		b.WriteByte('\n')
		b.WriteString(a.response.Render(width))
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

func (a *assistantMessageItem) SetFocused(focused bool) {
	if a.response != nil && a.response.focused != focused {
		a.response.SetFocused(focused)
		a.Bump()
	}
}

func (a *assistantMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if a.response == nil {
		return false
	}
	if a.response.HandleMouseClick(btn, x, y) {
		a.Bump()
		return true
	}
	return false
}

// ── Tool Call Item ──────────────────────────────────────────────────────────

// maxStreamingLines is the maximum number of output lines rendered during
// and after tool execution. A sliding window keeps only the latest lines.
const maxStreamingLines = 15

// toolCallItem renders a tool call with its execution status and output.
// It progresses through: assembling → executing → finished.
type toolCallItem struct {
	*list.Versioned
	styles     Styles
	id         string
	name       string
	args       strings.Builder // raw JSON fragments during streaming
	parsedArgs map[string]any  // set once UIToolCallCompleted arrives
	output     []string        // sliding window of latest output lines
	totalLines int             // total output lines received (may exceed len(output))
	result     *toolResult
	finished   bool
	request    *requestDetailItem
	response   *detailItem
}

type toolResult struct {
	content string
	isError bool
}

var _ list.Item = (*toolCallItem)(nil)
var _ list.MouseClickable = (*toolCallItem)(nil)
var _ list.Focusable = (*toolCallItem)(nil)

func (t *toolCallItem) Render(width int) string {
	var b strings.Builder
	if t.finished && t.result != nil {
		if t.result.isError {
			b.WriteString(t.styles.Chat.ToolCallErr.Render("✗ "))
		} else {
			b.WriteString(t.styles.Chat.ToolCallOk.Render("✓ "))
		}
	} else if len(t.output) > 0 {
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
		} else if t.name == "EnterPlanMode" {
			// Don't render the plan mode prompt content — just show ✓ EnterPlanMode
		} else if t.name == "ExitPlanMode" {
			// Strip <system-reminder>...</system-reminder> and "## Approved Plan:" prefix
			content := stripSystemReminder(t.result.content)
			content = strings.TrimPrefix(content, "## Approved Plan:\n")
			content = strings.TrimSpace(content)
			if content != "" {
				lines := strings.Split(content, "\n")
				if len(lines) > 100 {
					head := lines[:30]
					tail := lines[len(lines)-20:]
					omitted := len(lines) - 30 - 20
					content = strings.Join(head, "\n") +
						fmt.Sprintf("\n... [%d lines omitted] ...\n", omitted) +
						strings.Join(tail, "\n")
				}
				b.WriteString("\n  ")
				b.WriteString(strings.ReplaceAll(
					t.styles.Chat.ToolCallOutput.Render(content), "\n", "\n  ",
				))
			}
		} else {
			// Show last maxStreamingLines of result content with line count.
			renderOutputLines(&b, t.styles, t.result.content)
		}
	} else if len(t.output) > 0 {
		// Show last maxStreamingLines of streaming output.
		renderSlidingOutput(&b, t.styles, t.output, t.totalLines)
	}

	if t.request != nil {
		b.WriteByte('\n')
		b.WriteString(t.request.Render(width))
	}
	if t.response != nil {
		b.WriteByte('\n')
		b.WriteString(t.response.Render(width))
	}
	return b.String()
}

func (t *toolCallItem) Finished() bool { return t.finished }

func (t *toolCallItem) SetFocused(focused bool) {
	if t.response != nil && t.response.focused != focused {
		t.response.SetFocused(focused)
		t.Bump()
	}
}

func (t *toolCallItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if t.response == nil {
		return false
	}
	if t.response.HandleMouseClick(btn, x, y) {
		t.Bump()
		return true
	}
	return false
}

func (t *toolCallItem) AppendArgs(delta string) {
	t.args.WriteString(delta)
	t.Bump()
}

// ResetArgs replaces the streaming args buffer with the complete input.
// Called on ToolCallCompleted to avoid duplicate content from delta accumulation.
func (t *toolCallItem) ResetArgs(full string) {
	t.args.Reset()
	t.args.WriteString(full)
	t.Bump()
}

func (t *toolCallItem) AppendOutput(text string) {
	t.totalLines++
	if len(t.output) >= maxStreamingLines {
		t.output = t.output[1:]
	}
	t.output = append(t.output, text)
	t.Bump()
}

func (t *toolCallItem) SetResult(content string, isError bool) {
	t.result = &toolResult{content: content, isError: isError}
	t.finished = true
	t.Bump()
}

// renderSlidingOutput writes the sliding-window streaming output to b.
func renderSlidingOutput(b *strings.Builder, styles Styles, lines []string, totalLines int) {
	if totalLines > maxStreamingLines {
		b.WriteString("\n  ")
		b.WriteString(styles.Chat.ToolCallSummary.Render(
			fmt.Sprintf("... %d more lines ...", totalLines-maxStreamingLines),
		))
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		b.WriteString("\n  ")
		b.WriteString(styles.Chat.ToolCallOutput.Render(line))
	}
}

// renderOutputLines writes the last maxStreamingLines of result content to b.
func renderOutputLines(b *strings.Builder, styles Styles, content string) {
	allLines := strings.Split(content, "\n")
	// Remove trailing empty line from split
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	totalLines := len(allLines)
	showLines := allLines
	if totalLines > maxStreamingLines {
		showLines = allLines[totalLines-maxStreamingLines:]
		b.WriteString("\n  ")
		b.WriteString(styles.Chat.ToolCallSummary.Render(
			fmt.Sprintf("... %d more lines ...", totalLines-maxStreamingLines),
		))
	}
	for _, line := range showLines {
		if line == "" {
			continue
		}
		b.WriteString("\n  ")
		b.WriteString(styles.Chat.ToolCallOutput.Render(line))
	}
}

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>\s*`)

func stripSystemReminder(s string) string {
	return strings.TrimSpace(systemReminderRe.ReplaceAllString(s, ""))
}

// ── Detail Item ─────────────────────────────────────────────────────────────

// truncationRetryItem renders a one-line notice when output is truncated
// and the runtime is automatically retrying with a larger token limit.
type truncationRetryItem struct {
	*list.Versioned
	styles    Styles
	prev, now int
	request   *requestDetailItem
}

var _ list.Item = (*truncationRetryItem)(nil)

func (t *truncationRetryItem) Render(width int) string {
	text := fmt.Sprintf("Output truncated, retrying with %dK tokens...", t.now/1024)
	rendered := t.styles.Chat.ToolCallRun.Width(width).Render("↻ " + text)
	if t.request != nil {
		rendered += "\n" + t.request.Render(width)
	}
	return rendered
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

// ── Plan Item ─────────────────────────────────────────────────────────────

// planItem renders a plan file's markdown content in the chat list.
type planItem struct {
	*list.Versioned
	styles      Styles
	content     string // raw markdown plan content
	fileName    string // plan file name
	finished    bool
	streamingMd streamingMarkdown
}

var _ list.Item = (*planItem)(nil)

func (p *planItem) Render(width int) string {
	const indent = "  "
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	var b strings.Builder
	b.WriteString(p.styles.Chat.ToolCallOk.Render("✓ "))
	b.WriteString(p.styles.Chat.ToolCallName.Render("Plan: " + p.fileName))
	b.WriteString("\n")

	renderer := markdownRenderer(innerWidth)
	rendered := p.streamingMd.Render(p.content, innerWidth, renderer)
	for _, line := range strings.Split(rendered, "\n") {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (p *planItem) Finished() bool { return p.finished }

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
const maxThinkingPreviewLines = 200

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
// message items (user, assistant, detail, tool call) as protocol.Events arrive.
type Chat struct {
	list                *list.List
	styles              Styles
	currentAssistant    *assistantMessageItem
	currentThinking     *thinkingItem
	currentDetail       *detailItem
	currentRequest      *requestDetailItem
	currentRequestHost  list.Item
	loading             *loadingItem
	queuedItems         []*queuedMessageItem     // queued user messages while busy
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

func (c *Chat) lastItem() list.Item {
	return c.list.ItemAt(c.list.Len() - 1)
}

func (c *Chat) attachRequestDetail(host list.Item, detail *requestDetailItem) {
	if host == nil {
		return
	}
	switch item := host.(type) {
	case *userMessageItem:
		item.request = detail
		item.Bump()
	case *toolCallItem:
		item.request = detail
		item.Bump()
	case *truncationRetryItem:
		item.request = detail
		item.Bump()
	}
}

func (c *Chat) bumpRequestHost() {
	switch item := c.currentRequestHost.(type) {
	case *userMessageItem:
		item.Bump()
	case *toolCallItem:
		item.Bump()
	case *truncationRetryItem:
		item.Bump()
	}
}

func (c *Chat) attachResponseDetail(host list.Item, detail *detailItem) bool {
	if host == nil {
		return false
	}
	switch item := host.(type) {
	case *assistantMessageItem:
		if item == nil {
			return false
		}
		item.response = detail
		item.Bump()
		return true
	case *toolCallItem:
		if item == nil {
			return false
		}
		item.response = detail
		item.Bump()
		return true
	}
	return false
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

// AddQueued adds a queued user message to the chat (shown faint).
func (c *Chat) AddQueued(content string) {
	item := &queuedMessageItem{
		Versioned: list.NewVersioned(),
		styles:    c.styles,
		content:   content,
	}
	c.queuedItems = append(c.queuedItems, item)
	c.list.AppendItems(item)
	c.list.ScrollToBottom()
}

// PromoteQueued converts all queued items to regular user messages.
func (c *Chat) PromoteQueued() {
	for _, q := range c.queuedItems {
		q.Promote()
	}
	c.queuedItems = nil
}

// PromoteNextQueued promotes the first queued item (FIFO order).
// Called when the agent loop injects a queued input mid-turn.
func (c *Chat) PromoteNextQueued() {
	if len(c.queuedItems) == 0 {
		return
	}
	c.queuedItems[0].Promote()
	c.queuedItems = c.queuedItems[1:]
}

// RemoveQueued removes all queued items from the chat (used on cancel).
func (c *Chat) RemoveQueued() {
	for _, q := range c.queuedItems {
		for i := 0; i < c.list.Len(); i++ {
			if c.list.ItemAt(i) == q {
				c.list.RemoveItem(i)
				break
			}
		}
	}
	c.queuedItems = nil
}

// SetTokenCounts restores cumulative token counts (used on session resume).
func (c *Chat) SetTokenCounts(input, output int) {
	c.totalInputTokens = input
	c.totalOutputTokens = output
}

// SetContextUsed restores the last request input-token footprint.
func (c *Chat) SetContextUsed(input int) {
	c.currentInputTokens = input
}

// SetLoading adds a loading spinner to the chat, positioned after the last message.
func (c *Chat) SetLoading(item *loadingItem) {
	c.loading = item
	c.list.AppendItems(item)
	c.list.ScrollToBottom()
}

func (c *Chat) ensureLoading(label string) {
	if c.loading != nil {
		c.loading.label = label
		c.loading.Bump()
		return
	}
	c.SetLoading(&loadingItem{
		Versioned: list.NewVersioned(),
		styles:    c.styles,
		label:     label,
		startAt:   time.Now(),
	})
}

func (c *Chat) HasLoading() bool { return c.loading != nil }

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

// ApplyEvent applies a protocol.Event to update the chat state.
func (c *Chat) ApplyEvent(event protocol.Event) {
	switch e := event.(type) {
	case protocol.UserMessageAdded:
		c.currentAssistant = nil
		c.currentThinking = nil
		c.currentDetail = nil
		c.currentRequest = nil
		c.currentRequestHost = nil
		c.currentInputTokens = 0
		item := &userMessageItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			content:   e.Message.Content,
		}
		c.currentRequestHost = item
		c.list.AppendItems(item)

		c.list.ScrollToBottom()

	case protocol.SystemReminderAdded:
		c.currentAssistant = nil
		c.currentThinking = nil
		c.currentDetail = nil
		c.currentRequest = nil
		c.currentRequestHost = nil
		c.currentInputTokens = 0
		item := &systemReminderItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			content:   e.Content,
		}
		c.list.AppendItems(item)
		c.list.ScrollToBottom()

	case protocol.ModelRequestStarted:
		c.RemoveLoading()
		// Reset per-response state for the new API call.
		// Without this, a multi-turn loop where the second response has no
		// text (only tool_use) would attach its detail block to the previous
		// turn's assistant item.
		c.currentAssistant = nil
		c.currentThinking = nil
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
			reason:      e.Reason,
			inputTokens: e.EstimatedInputTokens,
			tokensExact: false,
			toolResults: e.ToolResults,
		}
		c.currentRequest = reqDetail
		c.currentRequestHost = c.lastItem()
		c.currentInputTokens = e.EstimatedInputTokens
		c.attachRequestDetail(c.currentRequestHost, reqDetail)
		loading := &loadingItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			label:     label,
			startAt:   time.Now(),
		}
		c.SetLoading(loading)

	case protocol.ThinkingStarted:
		c.RemoveLoading()
		c.currentThinking = &thinkingItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			expanded:  true,
		}
		c.list.AppendItems(c.currentThinking)
		c.list.ScrollToBottom()

	case protocol.ThinkingDelta:
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

	case protocol.ThinkingCompleted:
		if c.currentThinking != nil {
			c.currentThinking.Finish()
		}
		c.ensureLoading("Generating")

	case protocol.AssistantStarted:
		c.ensureLoading("Generating")
		c.currentAssistant = &assistantMessageItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
		}
		c.list.AppendItems(c.currentAssistant)
		c.list.ScrollToBottom()

	case protocol.AssistantDelta:
		if c.currentAssistant == nil {
			c.currentAssistant = &assistantMessageItem{
				Versioned: list.NewVersioned(),
				styles:    c.styles,
			}
			c.list.AppendItems(c.currentAssistant)
		}
		c.currentAssistant.AppendDelta(e.Text)
		c.list.ScrollToBottom()

	case protocol.StreamStarted:
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
			c.bumpRequestHost()
		}

	case protocol.StreamCompleted:
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
		if c.currentAssistant != nil {
			c.attachResponseDetail(c.currentAssistant, c.currentDetail)
		} else {
			c.attachResponseDetail(c.lastItem(), c.currentDetail)
		}
		c.list.ScrollToBottom()

	case protocol.TruncationRetry:
		c.list.AppendItems(&truncationRetryItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			prev:      e.PrevMaxTokens,
			now:       e.NewMaxTokens,
		})
		c.list.ScrollToBottom()

	case protocol.AssistantCompleted:
		if c.currentAssistant != nil {
			c.currentAssistant.Finish()
		}

	case protocol.RunFailed:
		c.RemoveLoading()
		if c.currentAssistant != nil {
			c.currentAssistant.Finish()
		}
		msg := "Interrupted"
		if e.Err != "" && e.Err != "context canceled" {
			msg = e.Err
		}
		c.list.AppendItems(&interruptedItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			message:   msg,
		})
		c.list.ScrollToBottom()

	case protocol.ToolCallStarted:
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

	case protocol.ToolCallDelta:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.AppendArgs(e.Delta)
		}

	case protocol.ToolCallCompleted:
		if item, ok := c.toolCalls[e.ID]; ok {
			// Replace the streaming buffer with the canonical complete JSON.
			// Deltas already assembled the same content, but rewriting avoids
			// duplicate appends and guarantees a clean string for fallback rendering.
			item.ResetArgs(string(e.Input))
			// Parse the complete JSON input into human-readable args.
			var parsed map[string]any
			if json.Unmarshal(e.Input, &parsed) == nil {
				item.parsedArgs = parsed
			}
			item.Bump()
		}

	case protocol.ToolExecStarted:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.Bump()
		}
		c.list.ScrollToBottom()

	case protocol.ToolExecDelta:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.AppendOutput(e.Text)
		}
		c.list.ScrollToBottom()

	case protocol.ToolExecCompleted:
		if item, ok := c.toolCalls[e.ID]; ok {
			item.SetResult(e.Result.Content, e.Result.IsError)
		}
		c.list.ScrollToBottom()

	case protocol.PlanApprovalRequested:
		c.RemoveLoading()
		item := &planItem{
			Versioned: list.NewVersioned(),
			styles:    c.styles,
			content:   e.PlanContent,
			fileName:  e.PlanFile,
			finished:  true,
		}
		c.list.AppendItems(item)
		c.list.ScrollToBottom()
	case protocol.TurnCompleted:
		c.RemoveLoading()
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
	} else if a, ok := item.(*assistantMessageItem); ok && a.response != nil {
		a.response.detail.Expanded = !a.response.detail.Expanded
		a.Bump()
	} else if tc, ok := item.(*toolCallItem); ok && tc.response != nil {
		tc.response.detail.Expanded = !tc.response.detail.Expanded
		tc.Bump()
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

// Clear resets the chat to an empty state, removing all items.
func (c *Chat) Clear() {
	width, height := c.list.Width(), c.list.Height()
	focused := c.list.Focused()
	c.list = list.NewList()
	c.list.SetGap(1)
	c.list.RegisterRenderCallback(list.FocusedRenderCallback(c.list))
	c.list.SetSize(width, height)
	if focused {
		c.list.Focus()
	}
	c.currentAssistant = nil
	c.currentThinking = nil
	c.currentDetail = nil
	c.currentRequest = nil
	c.currentRequestHost = nil
	c.loading = nil
	c.queuedItems = nil
	c.toolCalls = nil
	c.currentInputTokens = 0
	c.cacheCreationTokens = 0
	c.cacheReadTokens = 0
	c.totalInputTokens = 0
	c.totalOutputTokens = 0
}

// SetHistory replaces the chat content with historical messages.
// Each message is rendered as a finished (non-streaming) item.
func (c *Chat) SetHistory(messages []protocol.Message) {
	c.Clear()
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				c.list.AppendItems(&userMessageItem{
					Versioned: list.NewVersioned(),
					styles:    c.styles,
					content:   msg.Content,
				})
			} else if len(msg.ContentBlocks) > 0 {
				// Tool result messages have no Content but have ContentBlocks
				for _, block := range msg.ContentBlocks {
					if block.Type == protocol.ToolResultContentType && block.ToolResult != nil {
						item := &toolCallItem{
							Versioned: list.NewVersioned(),
							styles:    c.styles,
							name:      "tool_result",
							finished:  true,
							result:    &toolResult{content: block.ToolResult.Content, isError: block.ToolResult.IsError},
						}
						c.list.AppendItems(item)
					}
				}
			}
		case "assistant":
		// Render content blocks in order: thinking → text → tool_use.
		// Prefer ContentBlocks over msg.Content for structured rendering;
		// fall back to msg.Content only when no text-type blocks exist.
		var hasTextBlock bool
		for _, block := range msg.ContentBlocks {
			switch block.Type {
			case protocol.ThinkingContentType:
				if block.Text != "" {
					ti := &thinkingItem{
						Versioned: list.NewVersioned(),
						styles:    c.styles,
						expanded:  false,
						finished:  true,
					}
					ti.content.WriteString(block.Text)
					c.list.AppendItems(ti)
				}
			case protocol.TextContentType:
				hasTextBlock = true
				item := &assistantMessageItem{
					Versioned: list.NewVersioned(),
					styles:    c.styles,
					finished:  true,
				}
				item.content.WriteString(block.Text)
				c.list.AppendItems(item)
			case protocol.ToolUseContentType:
				if block.ToolUse != nil {
					var parsedArgs map[string]any
					json.Unmarshal(block.ToolUse.Input, &parsedArgs)
					tcItem := &toolCallItem{
						Versioned:  list.NewVersioned(),
						styles:     c.styles,
						id:         block.ToolUse.ID,
						name:       block.ToolUse.Name,
						parsedArgs: parsedArgs,
						finished:   true,
						result:     &toolResult{content: ""},
					}
					c.toolCalls[block.ToolUse.ID] = tcItem
					c.list.AppendItems(tcItem)
				}
			}
		}
		// Fallback: if no text-type block but msg.Content has text, render it.
		if !hasTextBlock && msg.Content != "" {
			item := &assistantMessageItem{
				Versioned: list.NewVersioned(),
				styles:    c.styles,
				finished:  true,
			}
			item.content.WriteString(msg.Content)
			c.list.AppendItems(item)
		}
		}
	}
	c.list.ScrollToBottom()
}
