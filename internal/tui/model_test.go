package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

func setupModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fk := tmux.NewFake()
	b := bus.New()
	t.Cleanup(b.Close)

	svc := service.NewSessionService(st, fk, b, nil)
	for _, name := range []string{"alpha", "beta"} {
		if _, err := svc.Create(context.Background(), service.CreateInput{
			Name:         name,
			ProjectPath:  "/tmp/" + name,
			WorktreePath: "/tmp/" + name,
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	m := NewModel(svc, st, b, theme.CatppuccinMocha(), config.Default())
	return m
}

func TestModel_View_RendersSeededSessions(t *testing.T) {
	m := setupModel(t)

	// Drive Init -> refresh -> View like Bubble Tea would.
	cmds := m.Init()
	if cmds == nil {
		t.Fatal("Init returned no command")
	}
	// Run the refresh tea.Cmd synchronously and feed the message.
	msg := runCmd(t, cmds)
	model, _ := m.Update(msg)
	m = model.(*Model)
	model, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = model.(*Model)

	out := m.View()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected output to contain 'alpha', got:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected output to contain 'beta', got:\n%s", out)
	}
}

// TestModel_CollapseExpand_NoRowDuplication reproduces the bug where
// toggling a group with h/l caused rows to duplicate on every
// keypress. The contract: the session-row count is invariant under
// any number of collapse/expand cycles.
func TestModel_CollapseExpand_NoRowDuplication(t *testing.T) {
	m := setupModel(t)

	cmds := m.Init()
	if cmds == nil {
		t.Fatal("Init returned no command")
	}
	msg := runCmd(t, cmds)
	model, _ := m.Update(msg)
	m = model.(*Model)
	model, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = model.(*Model)

	sessionRows := func() int {
		n := 0
		for _, r := range m.rows {
			if !r.IsGroup && !r.IsSubagent {
				n++
			}
		}
		return n
	}
	before := sessionRows()
	if before == 0 {
		t.Fatal("setup seeded zero sessions")
	}

	// Toggle the first group multiple times; the session-row count
	// must stay constant.
	for i := 0; i < 5; i++ {
		m.collapseCurrentGroup()
		m.expandCurrentGroup()
	}

	after := sessionRows()
	if after != before {
		t.Errorf("session rows drifted from %d to %d after toggles", before, after)
	}
}

// TestModel_OSCAttention_EdgeTriggered reproduces the bug where a
// level-triggered OSC scanner re-set the waiting state on every
// tick even after the hook cleared it. The contract after the
// fix: the same OSC sequence staying in the tmux visible buffer
// must NOT re-surface once it has been dismissed; only a new
// OSC (different title/body) breaks the suppression.
func TestModel_OSCAttention_EdgeTriggered(t *testing.T) {
	m := setupModel(t)
	ctx := context.Background()

	// Pick one seeded session.
	rows, _ := m.store.Sessions().List(ctx)
	if len(rows) == 0 {
		t.Fatal("no seeded sessions")
	}
	sess := rows[0]

	capture := "\x1b]777;notify;Need input;please approve\x07"

	// First tick: the OSC must set attention.
	m.applyAttentionHint(ctx, sess, capture)
	got, _ := m.store.Sessions().GetByID(ctx, sess.ID)
	if got.AttentionState != string(store.AttentionWaitingInput) {
		t.Fatalf("expected waiting_input after first OSC tick, got %q", got.AttentionState)
	}
	if got.AttentionTitle.String != "Need input" {
		t.Errorf("title = %q", got.AttentionTitle.String)
	}

	// Simulate hook clearing the attention (user typed something).
	_ = m.store.Sessions().SetAttention(ctx, sess.ID,
		store.AttentionNone, "", "", "")

	// Second tick: the OSC is STILL in the capture buffer, but we
	// have already shown it and it was cleared — the scanner must
	// stay silent.
	after, _ := m.store.Sessions().GetByID(ctx, sess.ID)
	m.applyAttentionHint(ctx, after, capture)
	again, _ := m.store.Sessions().GetByID(ctx, sess.ID)
	if again.AttentionState == string(store.AttentionWaitingInput) {
		t.Fatalf("suppression failed: same OSC re-surfaced after dismissal")
	}

	// Third tick: a NEW OSC (different body) must break the
	// suppression and re-trigger the waiting state.
	newCapture := "\x1b]777;notify;Need input;another request\x07"
	m.applyAttentionHint(ctx, again, newCapture)
	fresh, _ := m.store.Sessions().GetByID(ctx, sess.ID)
	if fresh.AttentionState != string(store.AttentionWaitingInput) {
		t.Fatalf("new OSC should fire even after earlier suppression, got %q", fresh.AttentionState)
	}
	if fresh.AttentionBody.String != "another request" {
		t.Errorf("body = %q", fresh.AttentionBody.String)
	}
}

// runCmd executes a tea.Cmd (or batch of commands) and returns the
// first non-nil tea.Msg it produces. It is sufficient for unit tests
// that just need to drive Init's refresh through to a Model update.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected a tea.Msg from cmd")
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		// Find the first non-tick message in the batch.
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			inner := sub()
			switch inner.(type) {
			case tickMsg, nil:
				continue
			default:
				return inner
			}
		}
	}
	return msg
}
