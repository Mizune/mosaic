package conductor_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/conductor"
)

var _ = Describe("Scheduler", func() {
	Describe("validation", func() {
		It("rejects an unknown dependency", func() {
			_, err := conductor.NewScheduler([]conductor.TaskSpec{
				{Name: "a", DependsOn: []string{"missing"}},
			})
			Expect(err).To(HaveOccurred())
		})

		It("rejects cycles", func() {
			_, err := conductor.NewScheduler([]conductor.TaskSpec{
				{Name: "a", DependsOn: []string{"b"}},
				{Name: "b", DependsOn: []string{"a"}},
			})
			Expect(err).To(MatchError(ContainSubstring("cycle")))
		})
	})

	Describe("ready set", func() {
		It("starts with every root task ready", func() {
			s, err := conductor.NewScheduler([]conductor.TaskSpec{
				{Name: "a"},
				{Name: "b"},
				{Name: "c", DependsOn: []string{"a", "b"}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Ready()).To(ConsistOf("a", "b"))
			Expect(s.State("c")).To(Equal(conductor.TaskPending))
		})

		It("promotes a dependent to ready when its last dependency completes", func() {
			s, _ := conductor.NewScheduler([]conductor.TaskSpec{
				{Name: "a"},
				{Name: "b"},
				{Name: "c", DependsOn: []string{"a", "b"}},
			})
			Expect(s.MarkRunning("a")).To(Succeed())
			newly, err := s.MarkDone("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(newly).To(BeEmpty()) // b still pending? No, b had indeg 0, so still ready.
			Expect(s.State("c")).To(Equal(conductor.TaskPending))

			Expect(s.MarkRunning("b")).To(Succeed())
			newly, err = s.MarkDone("b")
			Expect(err).NotTo(HaveOccurred())
			Expect(newly).To(ConsistOf("c"))
		})
	})

	Describe("Done", func() {
		It("reports false until every task is terminal", func() {
			s, _ := conductor.NewScheduler([]conductor.TaskSpec{{Name: "a"}, {Name: "b"}})
			Expect(s.Done()).To(BeFalse())

			Expect(s.MarkRunning("a")).To(Succeed())
			_, _ = s.MarkDone("a")
			Expect(s.Done()).To(BeFalse())

			Expect(s.MarkRunning("b")).To(Succeed())
			_, _ = s.MarkDone("b")
			Expect(s.Done()).To(BeTrue())
		})

		It("treats failed tasks as terminal", func() {
			s, _ := conductor.NewScheduler([]conductor.TaskSpec{{Name: "a"}})
			_, err := s.MarkFailed("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Done()).To(BeTrue())
		})

		It("cascades failure to every transitive dependent", func() {
			s, _ := conductor.NewScheduler([]conductor.TaskSpec{
				{Name: "a"},
				{Name: "b", DependsOn: []string{"a"}},
				{Name: "c", DependsOn: []string{"b"}},
			})
			cascaded, err := s.MarkFailed("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(cascaded).To(ConsistOf("b", "c"))
			Expect(s.State("b")).To(Equal(conductor.TaskFailed))
			Expect(s.State("c")).To(Equal(conductor.TaskFailed))
			Expect(s.Done()).To(BeTrue())
		})
	})
})
