package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

func fallbackRows(n int) []store.Artifact {
	rows := make([]store.Artifact, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, store.Artifact{ID: "a" + string(rune('0'+i)), SessionID: "s1", Kind: "mermaid", Source: "transcript", Title: "graph " + string(rune('A'+i)), CreatedAt: time.Now()})
	}
	return rows
}

func TestArtifactPicker_KeysWithoutStore(t *testing.T) {
	var q termSeqQueue
	deps := artifactPickerDeps{theme: theme.CatppuccinMocha(), fallback: fallbackRows(3), queueSeq: q.queue}
	p, cmd := newArtifactPicker(deps, "s1", "alpha")
	if cmd != nil {
		t.Fatal("no inline support: no initial command")
	}
	p.setSize(120, 40)
	if len(p.items) != 3 || p.cursor != 0 {
		t.Fatalf("items=%d cursor=%d", len(p.items), p.cursor)
	}
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	action, _, _ := p.updateKey(key("j"))
	if action != pickerActionNone || p.cursor != 1 || p.selectedID() != "a1" {
		t.Fatalf("j: action=%v cursor=%d", action, p.cursor)
	}
	p.updateKey(key("k"))
	p.updateKey(key("k"))
	if p.cursor != 0 {
		t.Fatalf("k must clamp at 0, got %d", p.cursor)
	}

	// Enter with no viewer explains instead of opening.
	action, notice, cmd := p.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != pickerActionNone || cmd != nil || notice.level != noticeInfo || !strings.Contains(notice.title, "viewer disabled") {
		t.Fatalf("enter without viewer: action=%v notice=%+v cmd=%v", action, notice, cmd != nil)
	}
	if !strings.Contains(p.view(), "this viewer") && !strings.Contains(p.view(), "viewer disabled") {
		t.Fatalf("right column should explain, got:\n%s", p.view())
	}

	action, _, cmd = p.updateKey(key("q"))
	if action != pickerActionClose || cmd != nil {
		t.Fatalf("q: action=%v cmd=%v", action, cmd != nil)
	}
}

func TestArtifactPicker_EnterOpensThroughViewer(t *testing.T) {
	var q termSeqQueue
	fake := &fakeArtifactViewer{}
	deps := artifactPickerDeps{theme: theme.CatppuccinMocha(), fallback: fallbackRows(1), viewer: fake, queueSeq: q.queue}
	p, _ := newArtifactPicker(deps, "s1", "alpha")
	p.setSize(100, 30)
	_, notice, cmd := p.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	if notice.level != noticeNone || cmd == nil {
		t.Fatalf("expected an open command, notice=%+v", notice)
	}
	handled, notice, _ := p.updateMsg(cmd())
	if !handled || notice.level != noticeInfo || !strings.Contains(notice.title, "opened graph A (fake)") {
		t.Fatalf("opened notice = %+v handled=%v", notice, handled)
	}
	if len(fake.opened) != 1 || fake.opened[0].ID != "a0" {
		t.Fatalf("viewer got %+v", fake.opened)
	}
	if handled, _, _ := p.updateMsg(tea.WindowSizeMsg{}); handled {
		t.Fatal("foreign messages must not be claimed")
	}
}

func TestArtifactPicker_ReloadKeepsSelection(t *testing.T) {
	m := setupModel(t)
	sess, err := m.store.Sessions().GetByName(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	old := seedArtifact(t, m, "alpha", "old")
	var q termSeqQueue
	deps := artifactPickerDeps{theme: theme.CatppuccinMocha(), store: m.store, queueSeq: q.queue}
	p, _ := newArtifactPicker(deps, sess.ID, "alpha")
	p.setSize(100, 30)
	if p.selectedID() != old.ID {
		t.Fatalf("selected %q, want %q", p.selectedID(), old.ID)
	}
	// A newer row lands first in the list; the selection must stay.
	if _, _, err := m.store.Artifacts().Upsert(context.Background(), store.NewArtifact{
		SessionID: sess.ID, Kind: "mermaid", Source: "transcript", Title: "newer", Content: "pie", ContentHash: "new", CreatedAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	p.reloadKeepingSelection()
	if len(p.items) != 2 || p.items[0].Title != "newer" {
		t.Fatalf("items = %+v", p.items)
	}
	if p.selectedID() != old.ID || p.cursor != 1 {
		t.Fatalf("selection moved: id=%q cursor=%d", p.selectedID(), p.cursor)
	}
}

func TestRenderModalFrame_CentresBody(t *testing.T) {
	out := renderModalFrame(theme.CatppuccinMocha(), 80, 20, "hello")
	if !strings.Contains(out, "hello") || strings.Count(out, "\n") < 19 {
		t.Fatalf("frame should fill the screen height, got %d lines", strings.Count(out, "\n")+1)
	}
}
