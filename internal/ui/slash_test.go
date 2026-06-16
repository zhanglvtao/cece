package ui

import "testing"

func TestParseSlashSpecRejectsDoubleSlash(t *testing.T) {
	spec := parseSlashSpec("//")
	if spec.Valid() {
		t.Fatalf("spec = %+v, want invalid", spec)
	}
}

func TestParseSlashSpecAcceptsNormalCommand(t *testing.T) {
	spec := parseSlashSpec("/model")
	if !spec.Valid() {
		t.Fatal("expected valid slash spec")
	}
	if spec.Command != "/model" {
		t.Fatalf("command = %q, want /model", spec.Command)
	}
}
