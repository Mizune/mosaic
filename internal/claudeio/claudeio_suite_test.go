package claudeio_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClaudeIO(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClaudeIO Suite")
}
