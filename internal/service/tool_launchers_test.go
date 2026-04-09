package service_test

import (
	"testing"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

func TestLaunchCommandForTool(t *testing.T) {
	cases := []struct {
		in   store.SessionTool
		want string
	}{
		{store.ToolClaude, "claude"},
		{store.ToolCodex, "codex"},
		{store.ToolGemini, "gemini"},
		{store.ToolShell, ""},
		{"", ""},
		{"aider", "aider"}, // unknown falls through to tool name
	}
	for _, c := range cases {
		if got := service.LaunchCommandForTool(c.in); got != c.want {
			t.Errorf("LaunchCommandForTool(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLaunchCommandForSession_AppendsConfiguredFlags(t *testing.T) {
	flags := config.ToolLaunchFlags{
		BypassPermissions: true,
		AutoMode:          true,
		ExtraArgs:         []string{"--model", "opus-4"},
	}
	got := service.LaunchCommandForSession(store.ToolClaude, "", flags, false)
	want := "claude --dangerously-skip-permissions --auto --model opus-4"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLaunchCommandForSession_NoFlagsForOtherTools(t *testing.T) {
	flags := config.ToolLaunchFlags{BypassPermissions: true, AutoMode: true}
	if got := service.LaunchCommandForSession(store.ToolCodex, "", flags, false); got != "codex" {
		t.Errorf("got %q, want codex (bypass/auto are claude-only)", got)
	}
}

func TestLaunchCommandForSession_ShellStaysEmpty(t *testing.T) {
	flags := config.ToolLaunchFlags{ExtraArgs: []string{"--anything"}}
	if got := service.LaunchCommandForSession(store.ToolShell, "", flags, false); got != "" {
		t.Errorf("shell launch should stay empty even with flags, got %q", got)
	}
}
