package live

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/store"
)

// Messenger is the slice of the tmux backend the notifier needs.
type Messenger interface {
	DisplayMessage(ctx context.Context, target, text string, delay time.Duration) error
}

// ArtifactNotifier tells a user who is attached to a session that a
// new artifact just landed, through tmux's status line, since the
// TUI's badge is out of sight while attached. It subscribes to
// ArtifactDetected, which the reconciler publishes only for rows that
// did not exist before, so the startup replay stays silent.
type ArtifactNotifier struct {
	store *store.Store
	bus   *bus.Bus
	tmux  Messenger
	log   *slog.Logger
	delay time.Duration

	unsubscribe func()
}

// notifyDelay is how long the status-line message stays visible.
const notifyDelay = 5 * time.Second

// maxNotifyTitle keeps the message on one status line.
const maxNotifyTitle = 60

// NewArtifactNotifier constructs a notifier; call Start to subscribe.
func NewArtifactNotifier(s *store.Store, b *bus.Bus, t Messenger) *ArtifactNotifier {
	return &ArtifactNotifier{
		store: s,
		bus:   b,
		tmux:  t,
		log:   slog.With("subsys", "live.notifier"),
		delay: notifyDelay,
	}
}

// Start subscribes to the bus. Calling it twice is a no-op.
func (n *ArtifactNotifier) Start() {
	if n.unsubscribe != nil {
		return
	}
	n.unsubscribe = n.bus.Subscribe(n.handle)
}

// Stop unsubscribes.
func (n *ArtifactNotifier) Stop() {
	if n.unsubscribe != nil {
		n.unsubscribe()
		n.unsubscribe = nil
	}
}

func (n *ArtifactNotifier) handle(e bus.Event) {
	ev, ok := e.(ArtifactDetected)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	row, err := n.store.Sessions().GetByID(ctx, ev.SessionID)
	if err != nil {
		n.log.Debug("artifact notification: session lookup", "session", ev.SessionID, "err", err)
		return
	}
	if err := n.tmux.DisplayMessage(ctx, row.TmuxSession, notifyText(ev), n.delay); err != nil {
		n.log.Warn("artifact notification", "session", row.Name, "err", err)
	}
}

// notifyText is the status-line message for one artifact.
func notifyText(ev ArtifactDetected) string {
	title := strings.TrimSpace(ev.Title)
	if title == "" {
		title = ev.Kind
	}
	if runes := []rune(title); len(runes) > maxNotifyTitle {
		title = string(runes[:maxNotifyTitle-1]) + "…"
	}
	return "◆ " + title + " — prefix+O to view"
}
