package live_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/live"
	"github.com/Mizune/mosaic/internal/tmux"
)

func waitForMessages(t *testing.T, fk *tmux.Fake, want int) []tmux.FakeMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		msgs := fk.Messages()
		if len(msgs) >= want {
			return msgs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d messages, have %d", want, len(msgs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestArtifactNotifier_ShowsStatusLineMessage(t *testing.T) {
	env := setupReconciler(t)
	fk := tmux.NewFake()
	if err := fk.NewSession(env.Ctx, tmux.NewSessionOptions{Name: env.Parent.TmuxSession}); err != nil {
		t.Fatal(err)
	}
	n := live.NewArtifactNotifier(env.Store, env.Bus, fk)
	n.Start()
	defer n.Stop()

	env.Bus.Publish(live.ArtifactDetected{SessionID: env.Parent.ID, ArtifactID: "x", Kind: "mermaid", Title: "graph TD"})
	msgs := waitForMessages(t, fk, 1)
	if msgs[0].Target != env.Parent.TmuxSession || msgs[0].Delay != 5*time.Second {
		t.Fatalf("message = %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Text, "graph TD") || !strings.Contains(msgs[0].Text, "prefix+O") {
		t.Fatalf("text = %q", msgs[0].Text)
	}

	// An unknown session produces nothing; a failing tmux call is
	// logged and the next event still gets through.
	env.Bus.Publish(live.ArtifactDetected{SessionID: "nope", Title: "ignored"})
	fk.FailOnce("message", errors.New("boom"))
	env.Bus.Publish(live.ArtifactDetected{SessionID: env.Parent.ID, Title: "dropped by failure"})
	env.Bus.Publish(live.ArtifactDetected{SessionID: env.Parent.ID, Kind: "image"})
	msgs = waitForMessages(t, fk, 2)
	time.Sleep(50 * time.Millisecond)
	msgs = fk.Messages()
	if len(msgs) != 2 || !strings.Contains(msgs[1].Text, "◆ image —") {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestArtifactNotifier_StartStopIdempotent(t *testing.T) {
	env := setupReconciler(t)
	n := live.NewArtifactNotifier(env.Store, env.Bus, tmux.NewFake())
	n.Start()
	n.Start()
	n.Stop()
	n.Stop()
}
