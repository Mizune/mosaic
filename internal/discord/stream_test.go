package discord

import (
	"testing"
	"time"
)

func TestDebouncerAllow(t *testing.T) {
	d := newDebouncer()

	// First call should always be allowed.
	if !d.allow("thread-1") {
		t.Error("first call should be allowed")
	}

	// Immediate second call should be denied.
	if d.allow("thread-1") {
		t.Error("immediate second call should be denied")
	}

	// Different thread should be allowed.
	if !d.allow("thread-2") {
		t.Error("different thread should be allowed")
	}
}

func TestDebouncerAllowAfterInterval(t *testing.T) {
	d := newDebouncer()

	// Override the last time to simulate passage of time.
	d.mu.Lock()
	d.last["thread-1"] = time.Now().Add(-3 * time.Second)
	d.mu.Unlock()

	if !d.allow("thread-1") {
		t.Error("should be allowed after debounce interval")
	}
}
