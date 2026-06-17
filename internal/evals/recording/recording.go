package recording

import (
	"context"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/tool"
)

// RecordingClient wraps a real ModelClient and records all Stream() calls into a cassette.
type RecordingClient struct {
	inner   agent.ModelClient
	turns   []CassetteTurn
}

// NewRecordingClient creates a recording wrapper around a real client.
func NewRecordingClient(inner agent.ModelClient) *RecordingClient {
	return &RecordingClient{inner: inner}
}

// Cassette returns the recorded cassette of all turns so far.
func (c *RecordingClient) Cassette() *Cassette {
	return &Cassette{Turns: c.turns}
}

// SetReasoningEffort delegates to the inner client.
func (c *RecordingClient) SetReasoningEffort(effort string) {
	c.inner.SetReasoningEffort(effort)
}

// Stream implements agent.ModelClient by forwarding to the real client and recording the events.
func (c *RecordingClient) Stream(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	ch, err := c.inner.Stream(ctx, messages, system, tools, maxTokens)
	if err != nil {
		return nil, err
	}

	out := make(chan agent.ApiStreamEvent, 64)
	go func() {
		defer close(out)
		turn := CassetteTurn{}
		for ev := range ch {
			turn.Events = append(turn.Events, fromApiEvent(ev))
			if ev.EventType == "message_start" && ev.InputTokens > 0 {
				turn.InputTokens = ev.InputTokens
			}
			if ev.EventType == "message_delta" && ev.OutputTokens > 0 {
				turn.OutputTokens = ev.OutputTokens
			}
			out <- ev
		}
		c.turns = append(c.turns, turn)
	}()
	return out, nil
}