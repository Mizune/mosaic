package worktree_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorktree(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Worktree Suite")
}
