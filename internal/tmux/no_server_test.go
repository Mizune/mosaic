package tmux_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/tmux"
)

// fakeTmux writes a script that prints stderrLine to stderr and exits 1,
// standing in for a tmux binary whose server is unreachable.
func fakeTmux(stderrLine string) string {
	dir := GinkgoT().TempDir()
	path := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho '" + stderrLine + "' >&2\nexit 1\n"
	Expect(os.WriteFile(path, []byte(script), 0o755)).To(Succeed())
	return path
}

var _ = Describe("CLI without a running server", func() {
	ctx := context.Background()

	DescribeTable("treats the message as no server",
		func(stderrLine string) {
			cli := &tmux.CLI{Binary: fakeTmux(stderrLine)}

			ok, err := cli.HasSession(ctx, "mos_a")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())

			names, err := cli.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(BeEmpty())

			Expect(cli.Kill(ctx, "mos_a")).To(Succeed())
			Expect(cli.KillServer(ctx)).To(Succeed())
		},
		// Newer tmux (seen on 3.8) after a reboot wiped the socket directory.
		Entry("socket missing", "error connecting to /private/tmp/tmux-501/mosaic (No such file or directory)"),
		// A stale socket file left behind by a crashed server.
		Entry("stale socket", "error connecting to /private/tmp/tmux-501/mosaic (Connection refused)"),
		Entry("classic wording", "no server running on /private/tmp/tmux-501/mosaic"),
	)

	It("still reports unrelated failures", func() {
		cli := &tmux.CLI{Binary: fakeTmux("unknown option -- z")}
		_, err := cli.HasSession(ctx, "mos_a")
		Expect(err).To(HaveOccurred())
		Expect(tmux.IsNotFound(err)).To(BeFalse())
	})
})
