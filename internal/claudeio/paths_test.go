package claudeio_test

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/claudeio"
)

var _ = Describe("Sanitize", func() {
	It("replaces slashes with dashes", func() {
		Expect(claudeio.Sanitize("/home/dev/Projects/mosaic")).
			To(Equal("-home-dev-Projects-mosaic"))
	})
})

var _ = Describe("FindLatestSession / ListSubagents", func() {
	var home string

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		dir := claudeio.ProjectDir(home, "/Users/test/foo")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	})

	It("returns os.ErrNotExist when there are no transcripts", func() {
		_, err := claudeio.FindLatestSession(home, "/Users/test/foo")
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("returns the most recently modified transcript", func() {
		dir := claudeio.ProjectDir(home, "/Users/test/foo")
		oldPath := filepath.Join(dir, "old.jsonl")
		newPath := filepath.Join(dir, "new.jsonl")
		Expect(os.WriteFile(oldPath, []byte("{}\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(newPath, []byte("{}\n"), 0o644)).To(Succeed())
		// Force a deterministic mtime ordering: old is far in the
		// past, new is "now".
		past := time.Now().Add(-1 * time.Hour)
		Expect(os.Chtimes(oldPath, past, past)).To(Succeed())

		got, err := claudeio.FindLatestSession(home, "/Users/test/foo")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("new"))
	})

	It("lists subagent ids", func() {
		dir := claudeio.SubagentDir(home, "/Users/test/foo", "abc")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		for _, name := range []string{"agent-aaa.jsonl", "agent-bbb.jsonl", "ignore.txt"} {
			Expect(os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644)).To(Succeed())
		}
		ids, err := claudeio.ListSubagents(home, "/Users/test/foo", "abc")
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(ConsistOf("aaa", "bbb"))
	})
})
