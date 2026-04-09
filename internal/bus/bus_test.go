package bus_test

import (
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/bus"
)

var _ = Describe("Bus", func() {
	var b *bus.Bus

	BeforeEach(func() {
		b = bus.New()
		DeferCleanup(b.Close)
	})

	Describe("Publish / Subscribe", func() {
		It("delivers events to a single subscriber", func() {
			received := make(chan bus.Event, 4)
			b.Subscribe(func(e bus.Event) { received <- e })

			b.Publish(bus.SessionStarted{SessionID: "abc"})
			Eventually(received, "1s").Should(Receive(BeAssignableToTypeOf(bus.SessionStarted{})))
		})

		It("fans out one event to every subscriber", func() {
			var (
				wg      sync.WaitGroup
				count   int32
				handler = func(bus.Event) {
					atomic.AddInt32(&count, 1)
					wg.Done()
				}
			)
			wg.Add(3)
			b.Subscribe(handler)
			b.Subscribe(handler)
			b.Subscribe(handler)

			b.Publish(bus.SessionStopped{SessionID: "x"})
			waitWithTimeout(&wg, time.Second)
			Expect(atomic.LoadInt32(&count)).To(Equal(int32(3)))
		})

		It("preserves order per subscriber", func() {
			received := make(chan string, 16)
			b.Subscribe(func(e bus.Event) {
				if s, ok := e.(bus.SessionStatus); ok {
					received <- s.Status
				}
			})

			for _, status := range []string{"a", "b", "c", "d"} {
				b.Publish(bus.SessionStatus{SessionID: "1", Status: status})
			}
			Eventually(received, "1s").Should(Receive(Equal("a")))
			Eventually(received, "1s").Should(Receive(Equal("b")))
			Eventually(received, "1s").Should(Receive(Equal("c")))
			Eventually(received, "1s").Should(Receive(Equal("d")))
		})

		It("stops delivering after Unsubscribe is called", func() {
			received := make(chan bus.Event, 4)
			unsubscribe := b.Subscribe(func(e bus.Event) { received <- e })

			b.Publish(bus.SessionStarted{SessionID: "1"})
			Eventually(received, "1s").Should(Receive())

			unsubscribe()
			b.Publish(bus.SessionStarted{SessionID: "2"})
			Consistently(received, "100ms").ShouldNot(Receive())
		})
	})

	Describe("backpressure", func() {
		It("drops oldest events instead of blocking when a subscriber is slow", func() {
			block := make(chan struct{})
			unblock := make(chan struct{})
			b.SubscribeWithBuffer(func(bus.Event) {
				<-block // first call blocks until released
			}, 2)

			// Two events fill the buffer; the next ones must be
			// dropped rather than block the publisher.
			done := make(chan struct{})
			go func() {
				for i := 0; i < 50; i++ {
					b.Publish(bus.SessionStatus{SessionID: "x", Status: "running"})
				}
				close(done)
				close(unblock)
			}()

			Eventually(done, "2s").Should(BeClosed())
			close(block) // release the slow handler

			Eventually(func() uint64 { return b.Stats().Dropped }, "2s").
				Should(BeNumerically(">", uint64(0)))
		})
	})

	Describe("Close", func() {
		It("stops delivery and is idempotent", func() {
			received := make(chan bus.Event, 4)
			b.Subscribe(func(e bus.Event) { received <- e })
			b.Publish(bus.SessionStarted{SessionID: "1"})
			Eventually(received, "1s").Should(Receive())

			b.Close()
			b.Close() // second Close must not panic
			b.Publish(bus.SessionStarted{SessionID: "2"})
			Consistently(received, "100ms").ShouldNot(Receive())
		})
	})
})

func waitWithTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		Fail("timed out waiting for handlers")
	}
}
