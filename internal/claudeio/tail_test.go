package claudeio_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/claudeio"
)

var _ = Describe("Tail", func() {
	It("does not hang on an oversized JSONL line", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "test.jsonl")

		validLine := `{"type":"assistant","uuid":"a","sessionId":"sid"}` + "\n"
		oversized := `{"type":"user","data":"` + strings.Repeat("x", 17*1024*1024) + `"}` + "\n"

		Expect(os.WriteFile(path, []byte(validLine+oversized), 0o644)).To(Succeed())

		tail := claudeio.NewTail(path)
		entries, err := tail.Read()
		Expect(err).NotTo(HaveOccurred())
		// At least the first valid entry should be returned.
		Expect(len(entries)).To(BeNumerically(">=", 1))
	})

	It("reads entries correctly after a malformed JSON line", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "test.jsonl")

		lines := strings.Join([]string{
			`{"type":"assistant","uuid":"a","sessionId":"sid"}`,
			`not valid json at all`,
			`{"type":"user","uuid":"b","sessionId":"sid"}`,
		}, "\n") + "\n"

		Expect(os.WriteFile(path, []byte(lines), 0o644)).To(Succeed())

		tail := claudeio.NewTail(path)
		entries, err := tail.Read()
		Expect(err).NotTo(HaveOccurred())
		// Both valid entries should be returned; the malformed line is skipped.
		Expect(entries).To(HaveLen(2))
	})
})
