package recording

import (
	"encoding/json"
	"os"

	"github.com/zhanglvtao/cece/internal/agent"
)

// Cassette records a complete agent interaction as a sequence of turns.
type Cassette struct {
	Turns []CassetteTurn `json:"turns"`
}

// CassetteTurn records the events from one Stream() call.
type CassetteTurn struct {
	InputTokens  int                `json:"input_tokens"`
	OutputTokens int                `json:"output_tokens"`
	Events       []CassetteEvent   `json:"events"`
}

// CassetteEvent is a JSON-serializable mirror of agent.ApiStreamEvent.
type CassetteEvent struct {
	Delta              string `json:"delta,omitempty"`
	Done               bool   `json:"done,omitempty"`
	Err                string `json:"err,omitempty"`
	EventType          string `json:"event_type,omitempty"`
	Detail             string `json:"detail,omitempty"`
	InputTokens        int    `json:"input_tokens,omitempty"`
	OutputTokens       int    `json:"output_tokens,omitempty"`
	StopReason         string `json:"stop_reason,omitempty"`
	CacheCreationTokens int   `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens    int    `json:"cache_read_tokens,omitempty"`
	ToolCallID         string `json:"tool_call_id,omitempty"`
	ToolCallName       string `json:"tool_call_name,omitempty"`
	ToolCallInput      string `json:"tool_call_input,omitempty"`
	Index              int    `json:"index,omitempty"`
	IsThinking         bool   `json:"is_thinking,omitempty"`
	ThinkingDelta      string `json:"thinking_delta,omitempty"`
	ThinkingSignature  string `json:"thinking_signature,omitempty"`
	IsRedactedThinking bool   `json:"is_redacted_thinking,omitempty"`
}

func fromApiEvent(e agent.ApiStreamEvent) CassetteEvent {
	ce := CassetteEvent{
		Delta:              e.Delta,
		Done:               e.Done,
		EventType:          string(e.EventType),
		Detail:             e.Detail,
		InputTokens:        e.InputTokens,
		OutputTokens:       e.OutputTokens,
		StopReason:         e.StopReason,
		CacheCreationTokens: e.CacheCreationTokens,
		CacheReadTokens:    e.CacheReadTokens,
		ToolCallID:         e.ToolCallID,
		ToolCallName:       e.ToolCallName,
		ToolCallInput:      e.ToolCallInput,
		Index:              e.Index,
		IsThinking:         e.IsThinking,
		ThinkingDelta:      e.ThinkingDelta,
		ThinkingSignature:  e.ThinkingSignature,
		IsRedactedThinking: e.IsRedactedThinking,
	}
	if e.Err != nil {
		ce.Err = e.Err.Error()
	}
	return ce
}

func (ce CassetteEvent) ToApiEvent() agent.ApiStreamEvent {
	var err error
	if ce.Err != "" {
		err = &replayError{ce.Err}
	}
	return agent.ApiStreamEvent{
		Delta:              ce.Delta,
		Done:               ce.Done,
		Err:                err,
		EventType:          agent.StreamEventType(ce.EventType),
		Detail:             ce.Detail,
		InputTokens:        ce.InputTokens,
		OutputTokens:       ce.OutputTokens,
		StopReason:         ce.StopReason,
		CacheCreationTokens: ce.CacheCreationTokens,
		CacheReadTokens:    ce.CacheReadTokens,
		ToolCallID:         ce.ToolCallID,
		ToolCallName:       ce.ToolCallName,
		ToolCallInput:      ce.ToolCallInput,
		Index:              ce.Index,
		IsThinking:         ce.IsThinking,
		ThinkingDelta:      ce.ThinkingDelta,
		ThinkingSignature:  ce.ThinkingSignature,
		IsRedactedThinking: ce.IsRedactedThinking,
	}
}

type replayError struct{ msg string }

func (e *replayError) Error() string { return e.msg }

// Save writes a cassette to a JSON file.
func Save(path string, c *Cassette) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads a cassette from a JSON file.
func Load(path string) (*Cassette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}