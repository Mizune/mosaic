package tui

import (
	"reflect"
	"testing"
)

func TestKeyMap_ApplyOverrides_ReplacesDefault(t *testing.T) {
	k := defaultKeys()
	k.applyOverrides(map[string]string{
		"quit":      "Q",
		"kill":      "x, X",
		"conductor": "C",
	})

	if !reflect.DeepEqual(k.Quit, []string{"Q"}) {
		t.Errorf("Quit = %v", k.Quit)
	}
	if !reflect.DeepEqual(k.Kill, []string{"x", "X"}) {
		t.Errorf("Kill = %v", k.Kill)
	}
	if !reflect.DeepEqual(k.Conductor, []string{"C"}) {
		t.Errorf("Conductor = %v", k.Conductor)
	}
	// Non-overridden binding stays at the default.
	if !reflect.DeepEqual(k.Help, []string{"?"}) {
		t.Errorf("Help should be untouched, got %v", k.Help)
	}
}

func TestKeyMap_ApplyOverrides_AliasFormsResolve(t *testing.T) {
	k := defaultKeys()
	// Both "mcp_manager" and "mcp-manager" should work.
	k.applyOverrides(map[string]string{"mcp-manager": "M"})
	if !reflect.DeepEqual(k.MCPManager, []string{"M"}) {
		t.Errorf("MCPManager = %v", k.MCPManager)
	}
}

func TestKeyMap_ApplyOverrides_UnknownKeyIgnored(t *testing.T) {
	k := defaultKeys()
	// Should not panic or error.
	k.applyOverrides(map[string]string{"does_not_exist": "Z"})
}

func TestKeyMap_ApplyOverrides_EmptyValueDisablesBinding(t *testing.T) {
	k := defaultKeys()
	k.applyOverrides(map[string]string{"filter": ""})
	if len(k.Filter) != 0 {
		t.Errorf("Filter should be disabled, got %v", k.Filter)
	}
}

func TestSplitChords_DropsEmpty(t *testing.T) {
	got := splitChords("up, k,, ")
	want := []string{"up", "k"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
