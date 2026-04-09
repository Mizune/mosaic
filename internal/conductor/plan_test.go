package conductor_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/conductor"
)

var _ = Describe("LoadPlan", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	writePlan := func(body string) string {
		p := filepath.Join(dir, "plan.toml")
		Expect(os.WriteFile(p, []byte(body), 0o644)).To(Succeed())
		return p
	}

	It("parses a minimal valid plan", func() {
		path := writePlan(`
[conductor]
name = "demo"

[[task]]
name = "first"
prompt = "do thing"

[[task]]
name = "second"
depends_on = ["first"]
prompt = "do other thing"
`)
		plan, err := conductor.LoadPlan(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Conductor.Name).To(Equal("demo"))
		Expect(plan.Tasks).To(HaveLen(2))
		Expect(plan.Tasks[1].DependsOn).To(Equal([]string{"first"}))
	})

	It("rejects a plan with a duplicate task name", func() {
		path := writePlan(`
[conductor]
name = "dup"

[[task]]
name = "x"
prompt = "a"

[[task]]
name = "x"
prompt = "b"
`)
		_, err := conductor.LoadPlan(path)
		Expect(err).To(MatchError(ContainSubstring("duplicate")))
	})

	It("rejects a plan with no tasks", func() {
		path := writePlan(`
[conductor]
name = "empty"
`)
		_, err := conductor.LoadPlan(path)
		Expect(err).To(MatchError(ContainSubstring("at least one")))
	})

	It("rejects a plan that depends on an unknown task", func() {
		path := writePlan(`
[conductor]
name = "broken"

[[task]]
name = "a"
depends_on = ["ghost"]
prompt = "x"
`)
		_, err := conductor.LoadPlan(path)
		Expect(err).To(MatchError(ContainSubstring("unknown task")))
	})

	It("rejects cyclic plans", func() {
		path := writePlan(`
[conductor]
name = "cyc"

[[task]]
name = "a"
depends_on = ["b"]
prompt = "x"

[[task]]
name = "b"
depends_on = ["a"]
prompt = "y"
`)
		_, err := conductor.LoadPlan(path)
		Expect(err).To(MatchError(ContainSubstring("cycle")))
	})
})
