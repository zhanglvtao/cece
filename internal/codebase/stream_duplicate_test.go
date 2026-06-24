package codebase

import (
	"strings"
	"testing"
)

func TestDecodeToolCallDuplicateStart(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Bash"}}]}`,
		``,
		`event: output`,
		`data: {"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{\"cmd"}}]}`,
		``,
		`event: output`,
		`data: {"tool_calls":[{"index":0,"type":"function","function":{"arguments":"\":\"ls\"}"}}]}`,
		``,
		`event: done`,
		`data: {"finish_reason":"tool_calls"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var startCount int
	var toolName, toolID string
	var inputParts []string

	for _, e := range events {
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			startCount++
			toolID = e.ToolCallID
			toolName = e.ToolCallName
		}
		if e.Detail == "input_json_delta" {
			inputParts = append(inputParts, e.ToolCallInput)
		}
	}

	t.Logf("startCount: %d", startCount)
	t.Logf("toolID: %s", toolID)
	t.Logf("toolName: %s", toolName)
	t.Logf("inputParts: %s", strings.Join(inputParts, ""))
}
