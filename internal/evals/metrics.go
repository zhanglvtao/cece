package evals

import (
	"strings"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/testkit"
)

// CollectMetrics derives protocol-agnostic metrics from a harness run.
func CollectMetrics(h *testkit.Harness, finalAnswer string) Metrics {
	metrics := Metrics{ReturnedAnswer: finalAnswer}
	for _, ev := range h.EventsSnapshot() {
		switch v := ev.(type) {
		case protocol.ToolExecCompleted:
			metrics.ToolCalls++
			if v.Name == "Read" {
				metrics.ReadCalls++
			}
			if strings.Contains(v.Result.Content, "Result artifact:") || strings.Contains(v.Result.Content, "Artifact:") {
				metrics.ArtifactRefsSeen++
			}
		case protocol.AssistantDelta:
			metrics.ContextCharacters += len(v.Text)
		}
	}
	metrics.ExpandedOnDemand = metrics.ReadCalls > 0
	return metrics
}

// BuildTranscript produces a compact textual replay from the recorded events.
func BuildTranscript(h *testkit.Harness) []string {
	events := h.EventsSnapshot()
	out := make([]string, 0, len(events))
	for _, ev := range events {
		switch v := ev.(type) {
		case protocol.UserMessageAdded:
			out = append(out, "user: "+strings.TrimSpace(v.Message.Content))
		case protocol.ToolExecCompleted:
			line := "tool:" + v.Name
			if v.Result.IsError {
				line += " error"
			}
			content := strings.TrimSpace(v.Result.Content)
			if content != "" {
				line += " => " + content
			}
			out = append(out, line)
		case protocol.AssistantDelta:
			text := strings.TrimSpace(v.Text)
			if text != "" {
				out = append(out, "assistantΔ: "+text)
			}
		case protocol.RunFailed:
			out = append(out, "run_failed: "+v.Err)
		}
	}
	return out
}
