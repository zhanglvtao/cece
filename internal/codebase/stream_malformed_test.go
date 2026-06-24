package codebase

import (
	"testing"
)

func TestDecodeMalformedProxyOutput(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"hi"}`,
		``,
		`_delta detail=text_delta delta="hi"`,
		`<id>some-uuid-here</id>`,
		`event: output`,
		`data: {"response":" there"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)
	
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for _, e := range events {
		if e.Delta != "" {
			text += e.Delta
		}
	}
	if text != "hi there" {
		t.Errorf("expected text 'hi there', got %q", text)
	}
}
