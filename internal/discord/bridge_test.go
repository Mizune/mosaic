package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestIsAuthorized_EmptyAllowList(t *testing.T) {
	br := &Bridge{cfg: Config{AllowedUserIDs: nil}}
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Member: &discordgo.Member{
				User: &discordgo.User{ID: "123"},
			},
		},
	}
	if !br.isAuthorized(i) {
		t.Error("empty allow list should allow everyone")
	}
}

func TestIsAuthorized_AllowedUser(t *testing.T) {
	br := &Bridge{cfg: Config{AllowedUserIDs: []string{"111", "222"}}}
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Member: &discordgo.Member{
				User: &discordgo.User{ID: "222"},
			},
		},
	}
	if !br.isAuthorized(i) {
		t.Error("user 222 should be authorized")
	}
}

func TestIsAuthorized_DeniedUser(t *testing.T) {
	br := &Bridge{cfg: Config{AllowedUserIDs: []string{"111", "222"}}}
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Member: &discordgo.Member{
				User: &discordgo.User{ID: "999"},
			},
		},
	}
	if br.isAuthorized(i) {
		t.Error("user 999 should be denied")
	}
}

func TestIsAuthorized_DMContext(t *testing.T) {
	br := &Bridge{cfg: Config{AllowedUserIDs: []string{"111"}}}
	// In DM context, Member is nil but User is set.
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			User: &discordgo.User{ID: "111"},
		},
	}
	if !br.isAuthorized(i) {
		t.Error("DM user 111 should be authorized")
	}
}

func TestIsAuthorized_NilMemberAndUser(t *testing.T) {
	br := &Bridge{cfg: Config{AllowedUserIDs: []string{"111"}}}
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	if br.isAuthorized(i) {
		t.Error("nil user should be denied when allow list is set")
	}
}

func TestThreadMapping(t *testing.T) {
	br := NewBridge(nil, nil, nil, nil, Config{})

	br.setThread("session-1", "thread-A")

	if got := br.threadFor("session-1"); got != "thread-A" {
		t.Errorf("threadFor = %q, want %q", got, "thread-A")
	}
	if got := br.sessionForThread("thread-A"); got != "session-1" {
		t.Errorf("sessionForThread = %q, want %q", got, "session-1")
	}

	// removeThread should clean up both maps.
	br.removeThread("session-1")

	if got := br.threadFor("session-1"); got != "" {
		t.Errorf("after remove, threadFor = %q, want empty", got)
	}
	if got := br.sessionForThread("thread-A"); got != "" {
		t.Errorf("after remove, sessionForThread = %q, want empty", got)
	}
}

func TestClaudeCache(t *testing.T) {
	br := NewBridge(nil, nil, nil, nil, Config{})

	// Populate cache manually.
	br.mu.Lock()
	br.claudeCache["claude-session-1"] = "session-1"
	br.mu.Unlock()

	br.setThread("session-1", "thread-A")

	// removeThread should also clear claude cache.
	br.removeThread("session-1")

	br.mu.RLock()
	_, exists := br.claudeCache["claude-session-1"]
	br.mu.RUnlock()

	if exists {
		t.Error("claude cache entry should be cleaned up after removeThread")
	}
}
