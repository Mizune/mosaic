package codexio_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/codexio"
)

func TestReadSessionMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	body := `{"bad":` + "\n" +
		`{"timestamp":"2026-05-22T01:02:03.000Z","type":"session_meta","payload":{"id":"sess-1","cwd":"/repo","cli_version":"0.133.0"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, ok := codexio.ReadSessionMeta(path)
	if !ok {
		t.Fatal("expected metadata")
	}
	if meta.ID != "sess-1" || meta.CWD != "/repo" || meta.CLIVersion != "0.133.0" || meta.Path != path {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestFindLatestSession(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "22")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, ts, cwd, id string) {
		t.Helper()
		body := `{"timestamp":"` + ts + `","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","cli_version":"0.133.0"}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rollout-old.jsonl", "2026-05-22T01:00:00Z", "/repo", "old")
	write("rollout-new.jsonl", "2026-05-22T02:00:00Z", "/repo/pkg", "new")
	write("rollout-other.jsonl", "2026-05-22T03:00:00Z", "/other", "other")

	earliest, _ := time.Parse(time.RFC3339, "2026-05-22T00:30:00Z")
	meta, ok := codexio.FindLatestSession(home, "/repo", earliest)
	if !ok {
		t.Fatal("expected latest session")
	}
	if meta.ID != "new" {
		t.Fatalf("got %q, want new", meta.ID)
	}
}
