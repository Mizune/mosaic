package live_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/live"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// reconcilerTestEnv bundles the objects most reconciler tests need.
type reconcilerTestEnv struct {
	Ctx    context.Context
	Store  *store.Store
	Bus    *bus.Bus
	Rec    *live.Reconciler
	Parent store.Session
}

// setupReconciler creates a store, bus, reconciler, and a parent
// session bound to "claude-sess". The bus and store are closed, and
// the reconciler is stopped via t.Cleanup.
func setupReconciler(t *testing.T) reconcilerTestEnv {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	parent, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert parent session: %v", err)
	}
	if err := st.Sessions().BindClaudeSession(ctx, parent.ID, "claude-sess"); err != nil {
		t.Fatalf("bind claude session id: %v", err)
	}
	b := bus.New()
	t.Cleanup(func() { b.Close() })
	rec := live.NewReconciler(st, b)
	rec.Start()
	t.Cleanup(func() { rec.Stop() })
	return reconcilerTestEnv{Ctx: ctx, Store: st, Bus: b, Rec: rec, Parent: parent}
}

// buildToolUseEntry constructs a transcript.Entry equivalent to one
// `assistant` line carrying a single tool_use block. It is shared by
// the live package's tests.
func buildToolUseEntry(id, name string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "assistant",
			Content: transcript.ContentList{
				{Type: "tool_use", ID: id, Name: name},
			},
		},
	}
}

// buildToolResultEntry constructs a user-role transcript entry that
// carries a tool_result block matching the given tool_use id. The
// state machine treats this as the "tool finished" signal.
func buildToolResultEntry(toolUseID string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryUser,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "user",
			Content: transcript.ContentList{
				{Type: "tool_result", ToolUseID: toolUseID},
			},
		},
	}
}

// buildAgentToolUseEntry constructs an assistant entry with an Agent
// tool_use block carrying structured input.
func buildAgentToolUseEntry(id, inputJSON string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "assistant",
			Content: transcript.ContentList{
				{Type: "tool_use", ID: id, Name: "Agent",
					Input: json.RawMessage(inputJSON)},
			},
		},
	}
}

// buildAgentToolResultEntry constructs a user-role entry with a
// tool_result for an Agent call. The content carries an agentId
// marker that the reconciler extracts.
func buildAgentToolResultEntry(toolUseID, content string, isError bool) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryUser,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "user",
			Content: transcript.ContentList{
				{Type: "tool_result", ToolUseID: toolUseID,
					Content: json.RawMessage(content), IsError: isError},
			},
		},
	}
}
