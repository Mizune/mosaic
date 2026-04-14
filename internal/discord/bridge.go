package discord

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

// Config holds the values the bridge needs at runtime. It is a
// snapshot copied from [config.DiscordSection] at construction time
// so the bridge never reaches back into the config tree.
type Config struct {
	Token             string
	ChannelID         string
	GuildID           string
	NotifyOnStop      bool
	NotifyOnAttention bool
	// AllowedUserIDs restricts who can invoke destructive slash
	// commands (kill, send). When empty, all guild members are
	// allowed.
	AllowedUserIDs []string
}

// Bridge connects mosaic to a Discord bot via discordgo. It
// subscribes to the event bus for outbound notifications/streaming
// and handles Discord interactions for inbound commands.
type Bridge struct {
	sessions  *service.SessionService
	messaging *service.MessagingService
	store     *store.Store
	bus       *bus.Bus
	log       *slog.Logger
	cfg       Config

	dg          *discordgo.Session
	unsubscribe func()
	commandIDs  []string // registered slash command IDs for cleanup

	// threads maps mosaic session IDs to Discord thread IDs.
	mu      sync.RWMutex
	threads map[string]string // sessionID → threadID
	// reverse maps Discord thread IDs to mosaic session IDs.
	reverse map[string]string // threadID → sessionID
	// claudeCache maps Claude session IDs to mosaic session IDs
	// to avoid full-table scans on every transcript event.
	claudeCache map[string]string // claudeSessionID → sessionID

	debouncer *threadDebouncer
}

// NewBridge constructs a Bridge. The bridge does nothing until
// [Bridge.Start] is called.
func NewBridge(
	sessions *service.SessionService,
	messaging *service.MessagingService,
	st *store.Store,
	b *bus.Bus,
	cfg Config,
) *Bridge {
	return &Bridge{
		sessions:    sessions,
		messaging:   messaging,
		store:       st,
		bus:         b,
		log:         slog.With("subsys", "discord.bridge"),
		cfg:         cfg,
		threads:     make(map[string]string),
		reverse:     make(map[string]string),
		claudeCache: make(map[string]string),
		debouncer:   newDebouncer(),
	}
}

// Start opens the Discord WebSocket, registers handlers, and
// subscribes to the event bus.
func (br *Bridge) Start(_ context.Context) error {
	if br.cfg.Token == "" {
		br.log.Info("discord bridge disabled (no token)")
		return nil
	}

	dg, err := discordgo.New("Bot " + br.cfg.Token)
	if err != nil {
		return err
	}
	dg.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent

	dg.AddHandler(br.handleInteraction)
	dg.AddHandler(br.handleMessageCreate)

	if err := dg.Open(); err != nil {
		return err
	}
	br.dg = dg

	br.unsubscribe = br.bus.Subscribe(br.handle)
	br.registerCommands()

	br.log.Info("discord bridge started",
		"channel", br.cfg.ChannelID,
		"guild", br.cfg.GuildID)

	return nil
}

// Stop deregisters slash commands, closes the Discord session, and
// unsubscribes from the bus.
func (br *Bridge) Stop() {
	if br.unsubscribe != nil {
		br.unsubscribe()
		br.unsubscribe = nil
	}
	br.deregisterCommands()
	if br.dg != nil {
		_ = br.dg.Close()
		br.dg = nil
	}
}

// isAuthorized checks whether a Discord user is allowed to invoke
// destructive commands. When AllowedUserIDs is empty, everyone is
// allowed (opt-in restriction).
func (br *Bridge) isAuthorized(i *discordgo.InteractionCreate) bool {
	if len(br.cfg.AllowedUserIDs) == 0 {
		return true
	}
	var userID string
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	for _, id := range br.cfg.AllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// threadFor returns the Discord thread ID for a mosaic session,
// or empty string if no thread exists.
func (br *Bridge) threadFor(sessionID string) string {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.threads[sessionID]
}

// sessionForThread returns the mosaic session ID for a Discord
// thread, or empty string if not tracked.
func (br *Bridge) sessionForThread(threadID string) string {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.reverse[threadID]
}

// setThread records a session-to-thread mapping.
func (br *Bridge) setThread(sessionID, threadID string) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.threads[sessionID] = threadID
	br.reverse[threadID] = sessionID
}

// removeThread removes thread and cache entries for a stopped session.
func (br *Bridge) removeThread(sessionID string) {
	// Collect threadID under bridge lock, then release before
	// touching the debouncer lock to avoid nested acquisition.
	br.mu.Lock()
	threadID := br.threads[sessionID]
	delete(br.threads, sessionID)
	delete(br.reverse, threadID)
	for k, v := range br.claudeCache {
		if v == sessionID {
			delete(br.claudeCache, k)
		}
	}
	br.mu.Unlock()

	if br.debouncer != nil && threadID != "" {
		br.debouncer.remove(threadID)
	}
}

// sendToThread posts a message to a Discord thread. Errors are
// logged but not returned — Discord delivery is best-effort.
func (br *Bridge) sendToThread(threadID, content string) {
	if br.dg == nil || threadID == "" || content == "" {
		return
	}
	if _, err := br.dg.ChannelMessageSend(threadID, content); err != nil {
		br.log.Warn("send to thread", "thread", threadID, "err", err)
	}
}

// sendEmbedToThread posts an embed to a Discord thread.
func (br *Bridge) sendEmbedToThread(threadID string, embed *discordgo.MessageEmbed) {
	if br.dg == nil || threadID == "" {
		return
	}
	if _, err := br.dg.ChannelMessageSendEmbed(threadID, embed); err != nil {
		br.log.Warn("send embed to thread", "thread", threadID, "err", err)
	}
}

// lookupSessionByClaudeID finds the mosaic session that owns the
// given Claude session ID, using an in-memory cache to avoid
// repeated full-table scans.
func (br *Bridge) lookupSessionByClaudeID(claudeSessionID string) (store.Session, bool) {
	if claudeSessionID == "" {
		return store.Session{}, false
	}

	// Check cache first.
	br.mu.RLock()
	sessionID, cached := br.claudeCache[claudeSessionID]
	br.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	if cached {
		s, err := br.store.Sessions().GetByID(ctx, sessionID)
		if err == nil {
			return s, true
		}
		// Cache stale — fall through to full scan.
	}

	rows, err := br.store.Sessions().List(ctx)
	if err != nil {
		return store.Session{}, false
	}
	for _, s := range rows {
		if s.ClaudeSessionID.Valid && s.ClaudeSessionID.String == claudeSessionID {
			br.mu.Lock()
			br.claudeCache[claudeSessionID] = s.ID
			br.mu.Unlock()
			return s, true
		}
	}
	return store.Session{}, false
}

// handleInteraction routes Discord slash commands.
func (br *Bridge) handleInteraction(_ *discordgo.Session, i *discordgo.InteractionCreate) {
	br.routeInteraction(i)
}

// handleMessageCreate handles messages in session threads.
func (br *Bridge) handleMessageCreate(_ *discordgo.Session, m *discordgo.MessageCreate) {
	br.routeMessage(m)
}
