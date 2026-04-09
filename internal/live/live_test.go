package live_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/live"
	"github.com/Mizune/mosaic/internal/store"
)

// Test that a hook NotificationEvent flips the parent session's
// attention state via the reconciler.
func TestReconciler_NotificationFlipsAttention(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	row, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	b := bus.New()
	defer b.Close()

	rec := live.NewReconciler(st, b)
	rec.Start()
	defer rec.Stop()

	b.Publish(hook.NotificationEvent{
		WorktreeID: row.ID,
		TabID:      "tab",
		SurfaceID:  "surf",
		Agent:      "claude",
		Event:      "Notification",
		Title:      "Permission required",
		Body:       "Bash needs approval",
	})

	// Give the dispatch goroutine a moment to land the write.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := st.Sessions().GetByID(ctx, row.ID)
		if err == nil && got.AttentionState == string(store.AttentionWaitingInput) {
			if got.AttentionTitle.String != "Permission required" {
				t.Fatalf("title = %q", got.AttentionTitle.String)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for attention state to flip")
}

// Test that the routine "Claude is waiting for your input"
// Notification hook payload does NOT pin the session into
// waiting_input — only real permission-style notifications do.
func TestReconciler_IgnoresIdleNotification(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	b := bus.New()
	defer b.Close()
	rec := live.NewReconciler(st, b)
	rec.Start()
	defer rec.Stop()

	b.Publish(hook.NotificationEvent{
		WorktreeID: row.ID,
		Agent:      "claude",
		Event:      "Notification",
		Title:      "Claude is waiting for your input",
		Body:       "",
	})

	// Give the dispatch goroutine a moment.
	time.Sleep(100 * time.Millisecond)
	got, _ := st.Sessions().GetByID(ctx, row.ID)
	if got.AttentionState == string(store.AttentionWaitingInput) {
		t.Fatalf("idle notification should not set waiting_input, got %q",
			got.AttentionTitle.String)
	}

	// And the real permission request STILL sets attention.
	b.Publish(hook.NotificationEvent{
		WorktreeID: row.ID,
		Agent:      "claude",
		Event:      "Notification",
		Title:      "Permission required",
		Body:       "Bash: rm -rf /",
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ = st.Sessions().GetByID(ctx, row.ID)
		if got.AttentionState == string(store.AttentionWaitingInput) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("real permission notification should set waiting_input")
}

// Test that a UserPromptSubmit busy=true event clears any lingering
// hook-sourced attention — the user engaging with the agent
// resolves whatever the prior cycle was waiting on.
func TestReconciler_BusyTrueClearsAttention(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Seed a hook-sourced attention.
	_ = st.Sessions().SetAttention(ctx, row.ID,
		store.AttentionWaitingInput, "Permission required", "Bash", "hook")

	b := bus.New()
	defer b.Close()
	rec := live.NewReconciler(st, b)
	rec.Start()
	defer rec.Stop()

	b.Publish(hook.BusyEvent{WorktreeID: row.ID, Active: true})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ := st.Sessions().GetByID(ctx, row.ID)
		if got.AttentionState == string(store.AttentionNone) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("busy=true should clear hook-sourced attention")
}

// Test that a TranscriptEntry for a sub-agent persists the running
// tool to the subagents row.
func TestReconciler_TranscriptEntryUpdatesSubagentTool(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	parent, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Bind the parent to a Claude session id so the reconciler can
	// route the events.
	if _, err := st.DB().ExecContext(ctx,
		`UPDATE sessions SET claude_session_id = ? WHERE id = ?`,
		"claude-sess", parent.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}

	b := bus.New()
	defer b.Close()
	rec := live.NewReconciler(st, b)
	rec.Start()
	defer rec.Stop()

	// Drive a tool_use through the bus.
	b.Publish(claudeio.TranscriptEntry{
		ProjectPath: "/tmp/p",
		SessionID:   "claude-sess",
		AgentID:     "deadbeef",
		Entry: buildToolUseEntry("id1", "Read"),
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sub, err := st.Subagents().GetByAgentID(ctx, parent.ID, "deadbeef")
		if err == nil && sub.CurrentTool.Valid && sub.CurrentTool.String == "Read" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for sub-agent tool to land")
}

// Test that a parent-session tool_use flips the mosaic session to
// running and a matching tool_result flips it back to idle. This
// is the path that matters when Claude Code hooks are NOT
// installed — the reconciler is the only thing that can move
// status back out of "running".
func TestReconciler_ParentStatusRunningAndIdleRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	parent, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := st.DB().ExecContext(ctx,
		`UPDATE sessions SET claude_session_id = ? WHERE id = ?`,
		"claude-sess", parent.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}

	b := bus.New()
	defer b.Close()
	rec := live.NewReconciler(st, b)
	rec.Start()
	defer rec.Stop()

	// tool_use on the parent → status should become running.
	b.Publish(claudeio.TranscriptEntry{
		ProjectPath: "/tmp/p",
		SessionID:   "claude-sess",
		// AgentID empty → parent transcript entry path.
		Entry: buildToolUseEntry("id1", "Read"),
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ := st.Sessions().GetByID(ctx, parent.ID)
		if got.Status == string(store.StatusRunning) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := st.Sessions().GetByID(ctx, parent.ID)
	if got.Status != string(store.StatusRunning) {
		t.Fatalf("want running, got %q", got.Status)
	}

	// tool_result on the parent → status should flip back to idle.
	b.Publish(claudeio.TranscriptEntry{
		ProjectPath: "/tmp/p",
		SessionID:   "claude-sess",
		Entry:       buildToolResultEntry("id1"),
	})

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ = st.Sessions().GetByID(ctx, parent.ID)
		if got.Status == string(store.StatusIdle) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want idle after tool_result, got %q", got.Status)
}

// Test that the Binder discovers a Claude session id from the
// transcript directory and persists it on the matching mosaic
// session.
func TestBinder_DiscoversAndBinds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	worktree := "/tmp/binder-target"
	row, err := st.Sessions().Insert(ctx, store.NewSession{
		Name: "p", WorktreePath: worktree,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Lay down a fake ~/.claude transcript inside an isolated home.
	// The binder filters candidate transcripts by first-entry
	// timestamp (so historical jsonl files can't hijack a fresh
	// mosaic session), so the fixture needs a non-zero timestamp
	// that falls at or after row.CreatedAt.
	home := t.TempDir()
	projDir := claudeio.ProjectDir(home, worktree)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	transcript := `{"type":"assistant","uuid":"a","sessionId":"sess-discovered","cwd":"` + worktree + `","timestamp":"` + ts + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, "sess-discovered.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	b := bus.New()
	defer b.Close()
	w := claudeio.NewWatcher(b, home)
	if err := w.Start(ctx); err != nil {
		t.Fatalf("watcher: %v", err)
	}
	defer w.Stop()

	binder := live.NewBinder(st, w, home)
	binder.Start(ctx)
	defer binder.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := st.Sessions().GetByID(ctx, row.ID)
		if err == nil && got.ClaudeSessionID.Valid && got.ClaudeSessionID.String == "sess-discovered" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for binder to bind the claude session id")
}
