package claudeio_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
)

var _ = Describe("Watcher", func() {
	var (
		ctx     context.Context
		home    string
		project string
		session string
		b       *bus.Bus
		w       *claudeio.Watcher

		entries   []claudeio.TranscriptEntry
		spawned   []claudeio.SubagentSpawned
		eventsMu  sync.Mutex
	)

	BeforeEach(func() {
		ctx = context.Background()
		home = GinkgoT().TempDir()
		project = "/Users/test/foo"
		session = "abc-123"
		entries = nil
		spawned = nil

		dir := claudeio.ProjectDir(home, project)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())

		b = bus.New()
		DeferCleanup(b.Close)

		b.Subscribe(func(e bus.Event) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			switch v := e.(type) {
			case claudeio.TranscriptEntry:
				entries = append(entries, v)
			case claudeio.SubagentSpawned:
				spawned = append(spawned, v)
			}
		})

		w = claudeio.NewWatcher(b, home)
		Expect(w.Start(ctx)).To(Succeed())
		DeferCleanup(w.Stop)
	})

	getEntries := func() []claudeio.TranscriptEntry {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		out := make([]claudeio.TranscriptEntry, len(entries))
		copy(out, entries)
		return out
	}
	getSpawned := func() []claudeio.SubagentSpawned {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		out := make([]claudeio.SubagentSpawned, len(spawned))
		copy(out, spawned)
		return out
	}

	It("emits the existing backlog as soon as Watch is called", func() {
		path := claudeio.SessionTranscriptPath(home, project, session)
		Expect(os.WriteFile(path, []byte(
			`{"type":"assistant","uuid":"a","sessionId":"`+session+`"}`+"\n"+
				`{"type":"user","uuid":"b","sessionId":"`+session+`"}`+"\n",
		), 0o644)).To(Succeed())

		Expect(w.Watch(project, session)).To(Succeed())

		Eventually(getEntries, time.Second).Should(HaveLen(2))
	})

	It("appends to the parent transcript and yields the new lines", func() {
		path := claudeio.SessionTranscriptPath(home, project, session)
		Expect(os.WriteFile(path, []byte(
			`{"type":"assistant","uuid":"a","sessionId":"`+session+`"}`+"\n",
		), 0o644)).To(Succeed())

		Expect(w.Watch(project, session)).To(Succeed())
		Eventually(getEntries, time.Second).Should(HaveLen(1))

		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		Expect(err).NotTo(HaveOccurred())
		_, err = f.WriteString(`{"type":"assistant","uuid":"c","sessionId":"` + session + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"id1","name":"Read"}]}}` + "\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())

		Eventually(getEntries, 2*time.Second).Should(HaveLen(2))
	})

	It("publishes SubagentSpawned when a new agent file appears", func() {
		path := claudeio.SessionTranscriptPath(home, project, session)
		Expect(os.WriteFile(path, []byte(`{"type":"system","uuid":"a","sessionId":"`+session+`"}`+"\n"), 0o644)).To(Succeed())
		Expect(w.Watch(project, session)).To(Succeed())

		subDir := claudeio.SubagentDir(home, project, session)
		Expect(os.MkdirAll(subDir, 0o755)).To(Succeed())
		subPath := filepath.Join(subDir, "agent-deadbeef.jsonl")
		Expect(os.WriteFile(subPath, []byte(
			`{"type":"assistant","uuid":"x","sessionId":"`+session+`","agentId":"deadbeef","isSidechain":true}`+"\n",
		), 0o644)).To(Succeed())

		Eventually(getSpawned, 2*time.Second).Should(HaveLen(1))
		Eventually(func() int {
			out := getEntries()
			n := 0
			for _, e := range out {
				if e.AgentID == "deadbeef" {
					n++
				}
			}
			return n
		}, 2*time.Second).Should(BeNumerically(">=", 1))
	})
})
