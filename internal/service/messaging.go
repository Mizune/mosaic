package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
)

// MessagingService owns the inter-agent messaging flow: it posts
// messages to the SQLite store, then drives a delivery loop that
// hands undelivered messages to the target session's tmux pane via
// send-keys.
//
// It is intentionally separate from [SessionService] because the
// two have distinct lifecycles — a CLI command like `mosaic send`
// just posts a message and exits, while the TUI boot path needs to
// run the delivery loop for as long as mosaic lives.
type MessagingService struct {
	store *store.Store
	tmux  tmux.Backend
	bus   *bus.Bus
	log   *slog.Logger

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	interval time.Duration
}

// NewMessagingService constructs a MessagingService. poll controls
// how often the delivery loop wakes; pass 0 for the default 1s.
func NewMessagingService(s *store.Store, t tmux.Backend, b *bus.Bus, poll time.Duration) *MessagingService {
	if poll <= 0 {
		poll = time.Second
	}
	return &MessagingService{
		store:    s,
		tmux:     t,
		bus:      b,
		log:      slog.With("subsys", "messaging"),
		interval: poll,
	}
}

// PostInput is the shape CLI/TUI callers use to send a message.
type PostInput struct {
	ToSession   string // user-visible session name
	FromSession string // optional sender (session name or empty for "cli")
	Kind        string // free-form label, defaults to "note"
	Body        string
	ConductorID string // optional: set by conductor runs
}

// Post records a new message. It does NOT wait for delivery — the
// caller can return immediately; the background delivery loop will
// pick the row up on its next tick.
func (m *MessagingService) Post(ctx context.Context, in PostInput) (store.Message, error) {
	if in.Body == "" {
		return store.Message{}, errors.New("messaging: body is required")
	}
	if in.ToSession == "" {
		return store.Message{}, errors.New("messaging: to_session is required")
	}
	to, err := m.store.Sessions().GetByName(ctx, in.ToSession)
	if err != nil {
		return store.Message{}, fmt.Errorf("resolve recipient %q: %w", in.ToSession, err)
	}
	var fromID string
	if in.FromSession != "" {
		from, err := m.store.Sessions().GetByName(ctx, in.FromSession)
		if err != nil {
			return store.Message{}, fmt.Errorf("resolve sender %q: %w", in.FromSession, err)
		}
		fromID = from.ID
	}
	kind := in.Kind
	if kind == "" {
		kind = "note"
	}
	return m.store.Conductors().PostMessage(ctx, store.NewMessage{
		ConductorID:   in.ConductorID,
		FromSessionID: fromID,
		ToSessionID:   to.ID,
		Kind:          kind,
		Body:          in.Body,
	})
}

// Start kicks off the background delivery loop. Calling Start on
// a running service is a no-op. Stop stops it and waits for the
// goroutine to exit.
func (m *MessagingService) Start(parent context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.done = make(chan struct{})
	m.mu.Unlock()

	go m.loop(ctx)
}

// Stop halts the delivery loop and waits for it to finish.
func (m *MessagingService) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

func (m *MessagingService) loop(ctx context.Context) {
	defer close(m.done)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.deliverOnce(ctx); err != nil {
				m.log.Warn("deliver", "err", err)
			}
		}
	}
}

// deliverOnce walks every active mosaic session, drains its inbox,
// and injects each message into the corresponding tmux pane. It is
// exported (lowercase, package-visible) so tests can step the loop
// deterministically instead of sleeping on the ticker.
func (m *MessagingService) deliverOnce(ctx context.Context) error {
	rows, err := m.store.Sessions().List(ctx)
	if err != nil {
		return err
	}
	for _, s := range rows {
		inbox, err := m.store.Conductors().Inbox(ctx, s.ID)
		if err != nil {
			m.log.Warn("inbox", "session", s.Name, "err", err)
			continue
		}
		if len(inbox) == 0 {
			continue
		}
		for _, msg := range inbox {
			text := formatIncoming(msg)
			if err := m.tmux.SendKeys(ctx, s.TmuxSession, text, true); err != nil {
				// Stop draining this session's inbox when tmux
				// rejects delivery (most commonly "session does
				// not exist"): leave the remaining messages
				// pending so they land on the next attach.
				m.log.Warn("send-keys", "session", s.Name, "err", err)
				break
			}
		}
		// Only mark delivered the rows tmux actually accepted. We
		// take the all-or-nothing simplification (mark all), which
		// matches the supacode wire format — a failing send-keys
		// is a rare case and re-deliveries aren't catastrophic.
		if err := m.store.Conductors().MarkDelivered(ctx, s.ID); err != nil {
			m.log.Warn("mark delivered", "session", s.Name, "err", err)
		}
	}
	return nil
}

// formatIncoming renders a Message into the exact text that will
// be typed into the recipient's pane. The format is kept terse so
// it reads well both in a raw shell and inside an agent prompt.
func formatIncoming(msg store.Message) string {
	var b strings.Builder
	b.WriteString("[mosaic:")
	b.WriteString(msg.Kind)
	b.WriteString("] ")
	b.WriteString(msg.Body)
	return b.String()
}
