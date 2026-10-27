package tmux_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("ParseEnv", func() {
	It("parses socket, pid and session index", func() {
		env, err := tmux.ParseEnv("/private/tmp/tmux-501/mosaic,4242,3")
		Expect(err).NotTo(HaveOccurred())
		Expect(env.Socket).To(Equal("/private/tmp/tmux-501/mosaic"))
		Expect(env.PID).To(Equal(4242))
		Expect(env.SessionIndex).To(Equal(3))
		Expect(env.SessionTarget()).To(Equal("$3"))
	})

	It("rejects values tmux would not produce", func() {
		for _, bad := range []string{"", "/tmp/sock", "/tmp/sock,4242", "/tmp/sock,abc,3", "/tmp/sock,4242,x", ",1,2", "/tmp/sock,0,1", "/tmp/sock,1,-1"} {
			_, err := tmux.ParseEnv(bad)
			Expect(err).To(HaveOccurred(), bad)
		}
	})
})
