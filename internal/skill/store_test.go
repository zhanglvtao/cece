package skill

import (
	"strings"
	"testing"
)

func TestStoreListingOnlyEnabled(t *testing.T) {
	skills := []*Skill{
		{Name: "brainstorming", Description: "brainstorm desc", Instructions: "inst1"},
		{Name: "cece-config", Description: "config desc", Instructions: "inst2"},
		{Name: "diagnose", Description: "diagnose desc", Instructions: "inst3"},
	}
	store := NewStore(skills)
	store.SetEnabled([]string{"brainstorming"})

	listing := store.Listing()
	if strings.Contains(listing, "cece-config") {
		t.Error("listing should not contain disabled skill 'cece-config'")
	}
	if strings.Contains(listing, "diagnose") {
		t.Error("listing should not contain disabled skill 'diagnose'")
	}
	if !strings.Contains(listing, "brainstorming") {
		t.Error("listing should contain enabled skill 'brainstorming'")
	}
}

func TestStoreSetEnabledEmptyEnablesAll(t *testing.T) {
	skills := []*Skill{
		{Name: "a", Description: "a", Instructions: "a"},
		{Name: "b", Description: "b", Instructions: "b"},
	}
	store := NewStore(skills)
	store.SetEnabled(nil) // nil = all enabled

	if !store.AllEnabled() {
		t.Error("nil SetEnabled should enable all")
	}
	if len(store.Enabled()) != 2 {
		t.Errorf("expected 2 enabled skills, got %d", len(store.Enabled()))
	}
}
