package tmux_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTmux(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tmux Suite")
}
