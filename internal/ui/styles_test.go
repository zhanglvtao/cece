package ui

import (
	"strings"
	"testing"
)

func TestStylesExist(t *testing.T) {
	s := DefaultStyles()

	// Chat.Assistant style should render text
	got := s.Chat.Assistant.Render("hello")
	if !strings.Contains(got, "hello") {
		t.Fatalf("Chat.Assistant.Render(\"hello\") = %q, want to contain \"hello\"", got)
	}

	// Detail style should be italic + faint
	got = s.Detail.Render("detail")
	if !strings.Contains(got, "detail") {
		t.Fatalf("Detail.Render(\"detail\") = %q, want to contain \"detail\"", got)
	}

	// Status style should render
	got = s.Status.Render("Ready")
	if !strings.Contains(got, "Ready") {
		t.Fatalf("Status.Render(\"Ready\") = %q, want to contain \"Ready\"", got)
	}

	// Chat.Divider style should render
	got = s.Chat.Divider.Render("─")
	if !strings.Contains(got, "─") {
		t.Fatalf("Chat.Divider.Render(\"─\") = %q, want to contain \"─\"", got)
	}

	// Input.Prompt style should render
	got = s.Input.Prompt.Render("> ")
	if !strings.Contains(got, ">") {
		t.Fatalf("Input.Prompt.Render(\"> \") = %q, want to contain \">\"", got)
	}
}

func TestDetailStyleIsItalicAndFaint(t *testing.T) {
	s := DefaultStyles()
	if !s.Detail.GetItalic() {
		t.Fatal("Detail style should be italic")
	}
	if !s.Detail.GetFaint() {
		t.Fatal("Detail style should be faint")
	}
}

func TestStatusStyleIsFaint(t *testing.T) {
	s := DefaultStyles()
	if !s.Status.GetFaint() {
		t.Fatal("Status style should be faint")
	}
}
