package live_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/live"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// waitForArtifacts polls the store until the parent session has at least
// want artifact rows, or fails the test after two seconds.
func waitForArtifacts(t *testing.T, env reconcilerTestEnv, want int) []store.Artifact {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := env.Store.Artifacts().ListBySession(env.Ctx, env.Parent.ID)
		if err != nil {
			t.Fatalf("list artifacts: %v", err)
		}
		if len(rows) >= want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d artifacts, have %d", want, len(rows))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mermaidEntry(ts time.Time) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: ts,
		Message: &transcript.Message{Role: "assistant", Content: transcript.ContentList{
			{Type: "text", Text: "Architecture:\n```mermaid\ngraph TD\n  A-->B\n```\n"},
		}},
	}
}

func TestReconciler_RecordsMermaidArtifactOnce(t *testing.T) {
	env := setupReconciler(t)
	ts := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	ev := claudeio.TranscriptEntry{ProjectPath: "/tmp/p", SessionID: "claude-sess", Entry: mermaidEntry(ts)}

	env.Bus.Publish(ev)
	rows := waitForArtifacts(t, env, 1)
	got := rows[0]
	if got.Kind != "mermaid" || got.Source != "transcript" || got.AgentID != "" {
		t.Fatalf("unexpected row: %+v", got)
	}
	if got.Title != "graph TD" || got.Content.String != "graph TD\n  A-->B" {
		t.Fatalf("title/content wrong: %q / %q", got.Title, got.Content.String)
	}
	if !got.CreatedAt.Equal(ts) {
		t.Fatalf("created_at should be the transcript timestamp, got %v", got.CreatedAt)
	}

	// Replaying the same entry (what Watcher does on every start) must
	// not create a second row.
	env.Bus.Publish(ev)
	time.Sleep(100 * time.Millisecond)
	if rows := waitForArtifacts(t, env, 1); len(rows) != 1 {
		t.Fatalf("expected replay to be idempotent, have %d rows", len(rows))
	}
}

func TestReconciler_RecordsWrittenImageFromSubagent(t *testing.T) {
	env := setupReconciler(t)
	entry := transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: time.Now(),
		CWD:       "/tmp/p",
		Message: &transcript.Message{Role: "assistant", Content: transcript.ContentList{
			{Type: "tool_use", ID: "w1", Name: "Write",
				Input: json.RawMessage(`{"file_path":"out/diagram.png","content":""}`)},
		}},
	}
	env.Bus.Publish(claudeio.TranscriptEntry{
		ProjectPath: "/tmp/p", SessionID: "claude-sess", AgentID: "abc123", Entry: entry,
	})
	rows := waitForArtifacts(t, env, 1)
	got := rows[0]
	if got.Kind != "image" || got.Path != "/tmp/p/out/diagram.png" || got.Title != "diagram.png" {
		t.Fatalf("unexpected row: %+v", got)
	}
	if got.AgentID != "abc123" {
		t.Fatalf("agent id should be recorded, got %q", got.AgentID)
	}
}

func TestReconciler_PublishesArtifactDetectedOnlyForNewRows(t *testing.T) {
	env := setupReconciler(t)
	var (
		mu     sync.Mutex
		events []live.ArtifactDetected
	)
	unsub := env.Bus.Subscribe(func(e bus.Event) {
		if ad, ok := e.(live.ArtifactDetected); ok {
			mu.Lock()
			events = append(events, ad)
			mu.Unlock()
		}
	})
	defer unsub()

	ev := claudeio.TranscriptEntry{ProjectPath: "/tmp/p", SessionID: "claude-sess", Entry: mermaidEntry(time.Now())}
	env.Bus.Publish(ev)
	rows := waitForArtifacts(t, env, 1)
	env.Bus.Publish(ev) // replay: no new row, no event
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected exactly one ArtifactDetected, got %d", len(events))
	}
	if events[0].SessionID != env.Parent.ID || events[0].ArtifactID != rows[0].ID || events[0].Kind != "mermaid" || events[0].Title != "graph TD" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestReconciler_IgnoresEntriesWithoutArtifacts(t *testing.T) {
	env := setupReconciler(t)
	env.Bus.Publish(claudeio.TranscriptEntry{
		ProjectPath: "/tmp/p", SessionID: "claude-sess", Entry: buildToolUseEntry("t1", "Read"),
	})
	time.Sleep(100 * time.Millisecond)
	rows, err := env.Store.Artifacts().ListBySession(env.Ctx, env.Parent.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no artifacts, got %+v", rows)
	}
}
