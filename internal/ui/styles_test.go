package ui

import (
	"strings"
	"testing"
)

func TestStylesExist(t *testing.T) {
	s := DefaultStyles()

	got := s.Chat.LabelAssistant.Render("hello")
	if !strings.Contains(got, "hello") {
		t.Fatalf("LabelAssistant.Render(\"hello\") = %q, want to contain \"hello\"", got)
	}

	got = s.Chat.LabelError.Render("err")
	if !strings.Contains(got, "err") {
		t.Fatalf("LabelError.Render(\"err\") = %q, want to contain \"err\"", got)
	}

	got = s.Headline.Render("Generating")
	if !strings.Contains(got, "Generating") {
		t.Fatalf("Headline.Render(\"Generating\") = %q", got)
	}

	got = s.Picker.Cursor.Render("> ")
	if !strings.Contains(got, ">") {
		t.Fatalf("Picker.Cursor.Render(\"> \") = %q", got)
	}
}
