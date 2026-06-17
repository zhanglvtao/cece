package recording

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/tool"
)

// ReplayClient implements agent.ModelClient by replaying a pre-recorded cassette.
type ReplayClient struct {
	mu    sync.Mutex
	turns []CassetteTurn
	calls atomic.Int32
}

// NewReplayClient creates a ReplayClient from a cassette.
func NewReplayClient(c *Cassette) *ReplayClient {
	return &ReplayClient{turns: c.Turns}
}

// Calls returns the number of Stream() invocations so far.
func (c *ReplayClient) Calls() int { return int(c.calls.Load()) }

// SetReasoningEffort is a no-op for the replay client.
func (c *ReplayClient) SetReasoningEffort(_ string) {}

// Stream implements agent.ModelClient by replaying the next turn from the cassette.
func (c *ReplayClient) Stream(_ context.Context, _ []agent.Message, _ agent.SystemPrompt, _ []tool.Definition, _ int) (<-chan agent.ApiStreamEvent, error) {
	idx := int(c.calls.Add(1)) - 1

	c.mu.Lock()
	defer c.mu.Unlock()

	if idx >= len(c.turns) {
		return nil, fmt.Errorf("replay_client: unrecorded turn %d (only %d turns in cassette)", idx+1, len(c.turns))
	}

	turn := c.turns[idx]
	out := make(chan agent.ApiStreamEvent, len(turn.Events))
	for _, ev := range turn.Events {
		out <- ev.ToApiEvent()
	}
	close(out)
	return out, nil
}