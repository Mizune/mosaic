package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

type fakeArtifactViewer struct {
	opened []store.Artifact
	err    error
}

func (f *fakeArtifactViewer) Name() string { return "fake" }
func (f *fakeArtifactViewer) Open(_ context.Context, a store.Artifact) error {
	f.opened = append(f.opened, a)
	return f.err
}

func seedArtifact(t *testing.T, m *Model, sessionName, hash string) store.Artifact {
	t.Helper()
	ctx := context.Background()
	sess, err := m.store.Sessions().GetByName(ctx, sessionName)
	if err != nil {
		t.Fatalf("session %s: %v", sessionName, err)
	}
	row, _, err := m.store.Artifacts().Upsert(ctx, store.NewArtifact{
		SessionID: sess.ID, Kind: "mermaid", Source: "transcript",
		Title: "graph TD", Content: "graph TD\nA-->B", ContentHash: hash,
	})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return row
}

func refreshModel(t *testing.T, m *Model) *Model {
	t.Helper()
	msg := runCmd(t, m.refreshCmd())
	res, _ := m.Update(msg)
	return res.(*Model)
}

// focusSession puts the cursor on the named session row. A refresh
// that raced the seeding (or failed transiently) can leave the row
// list stale, so it refreshes and retries a few times before failing.
func focusSession(t *testing.T, m *Model, name string) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		for i, r := range m.rows {
			if !r.IsGroup && !r.IsSubagent && r.Session.Name == name {
				m.cursor = i
				return
			}
		}
		refreshed := refreshModel(t, m)
		*m = *refreshed
	}
	t.Fatalf("session %q not found in rows (rows=%d, error=%q)", name, len(m.rows), m.error)
}

func typeString(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, string(r))
	}
	return m
}

func TestArtifactBadge_ShowsCountForFocusedSessionOnly(t *testing.T) {
	m := freshModel(t)
	seedArtifact(t, m, "alpha", "h1")
	m = refreshModel(t, m)

	focusSession(t, m, "alpha")
	out := m.View()
	if !strings.Contains(out, "◆ 1 artifact") {
		t.Fatalf("expected badge in tab strip, got:\n%s", out)
	}
	if !strings.Contains(out, "ARTIFACTS (1)") || !strings.Contains(out, "◇ graph TD") {
		t.Fatalf("expected Artifacts section in structured preview, got:\n%s", out)
	}

	focusSession(t, m, "beta")
	if out := m.View(); strings.Contains(out, "1 artifact") || strings.Contains(out, "ARTIFACTS (") {
		t.Fatalf("beta has no artifacts; badge and section must not show, got:\n%s", out)
	}
}

func TestKeyBinding_OOpensArtifactPicker(t *testing.T) {
	m := freshModel(t)
	m, _ = press(t, m, "o")
	if m.screen != ScreenArtifacts {
		t.Fatalf("o should open the artifact picker; screen=%d", m.screen)
	}
	if !strings.Contains(m.View(), "No artifacts yet") {
		t.Fatalf("expected empty state, got:\n%s", m.View())
	}
	m, _ = press(t, m, "q")
	if m.screen != ScreenTop {
		t.Fatalf("q should close the picker; screen=%d", m.screen)
	}
}

func TestArtifactPicker_EnterOpensViaViewer(t *testing.T) {
	m := freshModel(t)
	row := seedArtifact(t, m, "alpha", "h1")
	m = refreshModel(t, m)
	focusSession(t, m, "alpha")
	fake := &fakeArtifactViewer{}
	m.SetArtifactViewer(fake)

	m, _ = press(t, m, "o")
	if m.screen != ScreenArtifacts {
		t.Fatalf("screen=%d", m.screen)
	}
	if len(m.artifactPanel.items) != 1 || m.artifactPanel.items[0].ID != row.ID {
		t.Fatalf("picker items = %+v", m.artifactPanel.items)
	}
	if !strings.Contains(m.View(), "▶ ◇ graph TD") {
		t.Fatalf("expected highlighted row, got:\n%s", m.View())
	}

	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("enter should return an open command")
	}
	res, _ := m.Update(cmd())
	m = res.(*Model)
	if len(fake.opened) != 1 || fake.opened[0].ID != row.ID {
		t.Fatalf("viewer received %+v", fake.opened)
	}
	if !strings.Contains(m.topToast, "opened graph TD (fake)") {
		t.Fatalf("toast = %q", m.topToast)
	}

	m, _ = press(t, m, "esc")
	if m.screen != ScreenTop {
		t.Fatalf("esc should close; screen=%d", m.screen)
	}
}

func TestArtifactPicker_WithoutViewerShowsToast(t *testing.T) {
	m := freshModel(t)
	seedArtifact(t, m, "alpha", "h1")
	m = refreshModel(t, m)
	focusSession(t, m, "alpha")
	m.SetArtifactViewer(nil) // what [viewer] enabled = false produces

	m, _ = press(t, m, "o")
	m, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("no viewer configured: enter must not return a command")
	}
	if !strings.Contains(m.topToast, "viewer disabled") {
		t.Fatalf("toast = %q", m.topToast)
	}
}

func TestArtifactPicker_ViewerErrorLandsInErrorLog(t *testing.T) {
	m := freshModel(t)
	seedArtifact(t, m, "alpha", "h1")
	m = refreshModel(t, m)
	focusSession(t, m, "alpha")
	m.SetArtifactViewer(&fakeArtifactViewer{err: errors.New("cmux: socket closed")})

	m, _ = press(t, m, "o")
	m, cmd := press(t, m, "enter")
	res, _ := m.Update(cmd())
	m = res.(*Model)
	if m.error != "open artifact failed" {
		t.Fatalf("error = %q", m.error)
	}
	if m.screen != ScreenArtifacts {
		t.Fatal("a failed open must keep the picker open")
	}
}

func TestArtifactPicker_GroupRowOpensFirstSession(t *testing.T) {
	m := freshModel(t)
	groupIdx, firstSession := -1, ""
	for i, r := range m.rows {
		if r.IsGroup && groupIdx < 0 {
			groupIdx = i
			continue
		}
		if groupIdx >= 0 && !r.IsGroup && !r.IsSubagent {
			firstSession = r.Session.Name
			break
		}
	}
	if groupIdx < 0 || firstSession == "" {
		t.Skip("no group header with a session in this layout")
	}
	m.cursor = groupIdx
	m, _ = press(t, m, "o")
	if m.screen != ScreenArtifacts {
		t.Fatalf("o on a group header should open the picker for its first session; screen=%d", m.screen)
	}
	if m.artifactPanel.sessionName != firstSession || m.rows[m.cursor].Session.Name != firstSession {
		t.Fatalf("expected %q, got panel=%q cursor=%q", firstSession, m.artifactPanel.sessionName, m.rows[m.cursor].Session.Name)
	}
}

func TestArtifactPicker_CollapsedGroupIsExpandedFirst(t *testing.T) {
	m := freshModel(t)
	groupIdx := -1
	for i, r := range m.rows {
		if r.IsGroup {
			groupIdx = i
			break
		}
	}
	if groupIdx < 0 {
		t.Skip("no group rows in this layout")
	}
	name := m.rows[groupIdx].GroupName
	m.groupExpanded[name] = false
	m.rebuildRows()
	m.cursor = groupIdx
	m, _ = press(t, m, "o")
	if m.screen != ScreenArtifacts || !m.groupExpanded[name] {
		t.Fatalf("collapsed group should expand and open; screen=%d expanded=%v", m.screen, m.groupExpanded[name])
	}
}

func TestArtifactInput_ManualFilePersistsAndOpens(t *testing.T) {
	m := freshModel(t)
	focusSession(t, m, "alpha")
	fake := &fakeArtifactViewer{}
	m.SetArtifactViewer(fake)
	png := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(png, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	m, _ = press(t, m, "o")
	m, _ = press(t, m, "+")
	if !m.artifactPanel.input.active {
		t.Fatal("+ should activate the input")
	}
	m = typeString(t, m, png)
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("enter on a valid path should return an open command; err=%q", m.artifactPanel.input.err)
	}
	res, _ := m.Update(cmd())
	m = res.(*Model)

	items := m.artifactPanel.items
	if len(items) != 1 || items[0].Source != "manual" || items[0].Kind != "image" || items[0].Path != png {
		t.Fatalf("picker items = %+v", items)
	}
	if m.artifactPanel.input.active {
		t.Fatal("input should close after a successful submit")
	}
	if len(fake.opened) != 1 || fake.opened[0].Path != png {
		t.Fatalf("viewer received %+v", fake.opened)
	}
	// Submitting the same path again is idempotent.
	m, _ = press(t, m, "+")
	m = typeString(t, m, png)
	m, _ = press(t, m, "enter")
	if len(m.artifactPanel.items) != 1 {
		t.Fatalf("duplicate manual path must not add a row: %+v", m.artifactPanel.items)
	}
}

func TestArtifactInput_URLBecomesURLArtifact(t *testing.T) {
	m := freshModel(t)
	focusSession(t, m, "alpha")
	m.SetArtifactViewer(&fakeArtifactViewer{})
	m, _ = press(t, m, "o")
	m, _ = press(t, m, "+")
	m = typeString(t, m, "https://example.com/report")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("expected open command, err=%q", m.artifactPanel.input.err)
	}
	if items := m.artifactPanel.items; len(items) != 1 || items[0].Kind != "url" || items[0].Path != "https://example.com/report" {
		t.Fatalf("items = %+v", items)
	}
}

func TestArtifactInput_MissingPathShowsErrorAndStaysOpen(t *testing.T) {
	m := freshModel(t)
	focusSession(t, m, "alpha")
	m.SetArtifactViewer(&fakeArtifactViewer{})
	m, _ = press(t, m, "o")
	m, _ = press(t, m, "+")
	m = typeString(t, m, "/definitely/missing/x.png")
	m, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("missing file must not produce an open command")
	}
	in := m.artifactPanel.input
	if !in.active || in.err == "" {
		t.Fatalf("expected input to stay open with an error, got %+v", in)
	}
	if m.screen != ScreenArtifacts {
		t.Fatalf("screen=%d", m.screen)
	}
	if !strings.Contains(m.View(), "✗ ") {
		t.Fatalf("error should be rendered, got:\n%s", m.View())
	}
}

func TestArtifactInput_EditingKeys(t *testing.T) {
	m := freshModel(t)
	focusSession(t, m, "alpha")
	m, _ = press(t, m, "o")
	m, _ = press(t, m, "+")
	m = typeString(t, m, "ab c")
	if got := m.artifactPanel.input.value; got != "ab c" {
		t.Fatalf("value = %q", got)
	}
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = res.(*Model)
	if got := m.artifactPanel.input.value; got != "ab " {
		t.Fatalf("after backspace = %q", got)
	}
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = res.(*Model)
	if got := m.artifactPanel.input.value; got != "" {
		t.Fatalf("after ctrl+u = %q", got)
	}
	m, _ = press(t, m, "esc")
	if m.artifactPanel.input.active || m.screen != ScreenArtifacts {
		t.Fatal("esc in the input cancels the input, not the picker")
	}
}

func TestManualArtifactInput(t *testing.T) {
	dir := t.TempDir()
	mmd := filepath.Join(dir, "flow.mmd")
	if err := os.WriteFile(mmd, []byte("  graph LR\nA-->B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.mermaid")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		raw      string
		wantKind string
		wantErr  bool
	}{
		{"https", "https://example.com/x", "url", false},
		{"http", "HTTP://example.com/x", "url", false},
		{"file url", "file:///tmp/x.html", "url", false},
		{"mermaid file", mmd, "mermaid", false},
		{"empty mermaid", empty, "", true},
		{"unsupported ext", src, "", true},
		{"directory", dir, "", true},
		{"missing", filepath.Join(dir, "nope.png"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := manualArtifactInput(tc.raw, "s1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if in.Kind != tc.wantKind || in.Source != "manual" || in.SessionID != "s1" || in.ContentHash == "" {
				t.Fatalf("got %+v", in)
			}
			if tc.wantKind == "mermaid" && (in.Content != "graph LR\nA-->B" || in.Title != "flow.mmd") {
				t.Fatalf("mermaid input = %+v", in)
			}
		})
	}
}

func TestScrollWindowAndClipRunes(t *testing.T) {
	if s, e := scrollWindow(0, 3, 10); s != 0 || e != 3 {
		t.Fatalf("small list: %d %d", s, e)
	}
	if s, e := scrollWindow(0, 20, 5); s != 0 || e != 5 {
		t.Fatalf("top: %d %d", s, e)
	}
	if s, e := scrollWindow(10, 20, 5); s != 8 || e != 13 {
		t.Fatalf("middle: %d %d", s, e)
	}
	if s, e := scrollWindow(19, 20, 5); s != 15 || e != 20 {
		t.Fatalf("bottom: %d %d", s, e)
	}
	if got := clipRunes("日本語のタイトル", 4); got != "日本語…" {
		t.Fatalf("clipRunes = %q", got)
	}
	if got := clipRunes("short", 10); got != "short" {
		t.Fatalf("clipRunes = %q", got)
	}
}

func TestNewModel_ViewerWiring(t *testing.T) {
	m := setupModel(t)
	if m.artifactViewer == nil || m.artifactViewer.Name() == "" {
		t.Fatal("default config must wire a viewer backend")
	}
	cfg := config.Default()
	cfg.Viewer.Enabled = config.BoolPtr(false)
	disabled := NewModel(nil, nil, nil, theme.CatppuccinMocha(), cfg)
	if disabled.artifactViewer != nil {
		t.Fatal("[viewer] enabled = false must leave the viewer nil")
	}
}

func TestAutoOpen_OpensNewestNewArtifactAfterFirstRefresh(t *testing.T) {
	m := freshModel(t)
	m.config.Viewer.AutoOpen = true
	fake := &fakeArtifactViewer{}
	m.SetArtifactViewer(fake)

	seedArtifact(t, m, "alpha", "h1")
	res, cmd := m.Update(runCmd(t, m.refreshCmd()))
	m = res.(*Model)
	if cmd == nil {
		t.Fatal("a new artifact with auto_open on must return an open command")
	}
	res, _ = m.Update(cmd())
	m = res.(*Model)
	if len(fake.opened) != 1 || fake.opened[0].ContentHash != "h1" {
		t.Fatalf("viewer received %+v", fake.opened)
	}

	// Nothing new: nothing opens.
	_, cmd = m.Update(runCmd(t, m.refreshCmd()))
	if cmd != nil {
		t.Fatal("unchanged snapshot must not open anything")
	}
}

func TestAutoOpen_SkipsFirstRefreshBacklog(t *testing.T) {
	m := setupModel(t)
	m.config.Viewer.AutoOpen = true
	m.SetArtifactViewer(&fakeArtifactViewer{})
	seedArtifact(t, m, "alpha", "h1")

	_, cmd := m.Update(runCmd(t, m.Init()))
	if cmd != nil {
		t.Fatal("the first refresh carries the replayed backlog and must not auto-open")
	}
}

func TestAutoOpen_OffByDefaultAndIgnoresManualRows(t *testing.T) {
	m := freshModel(t)
	fake := &fakeArtifactViewer{}
	m.SetArtifactViewer(fake)
	seedArtifact(t, m, "alpha", "h1")
	if _, cmd := m.Update(runCmd(t, m.refreshCmd())); cmd != nil {
		t.Fatal("auto_open defaults to false")
	}

	m.config.Viewer.AutoOpen = true
	sess, err := m.store.Sessions().GetByName(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.store.Artifacts().Upsert(context.Background(), store.NewArtifact{
		SessionID: sess.ID, Kind: "url", Source: "manual", Title: "x", Path: "https://x", ContentHash: "m1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, cmd := m.Update(runCmd(t, m.refreshCmd())); cmd != nil {
		t.Fatal("manual rows were opened on submit; auto_open must skip them")
	}
	if len(fake.opened) != 0 {
		t.Fatalf("nothing should have opened, got %+v", fake.opened)
	}
}
