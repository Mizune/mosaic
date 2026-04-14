// Package tui implements mosaic's lazygit-style terminal UI.
//
// The design is intentionally minimal at the framework level: one
// Bubble Tea Model owns the entire screen, branches between TOP /
// L1 / L2 / modal views in its View method, and consumes typed
// events from the in-process [internal/bus] via a small adapter that
// translates bus events into tea.Msg values.
package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// Screen identifies which view the model is currently rendering.
type Screen int

const (
	ScreenTop Screen = iota
	ScreenHelp
	ScreenNewSession
	// ScreenConfirm is a generic "y/n" overlay used by destructive
	// actions (kill, delete, remove worktree) to avoid accidental
	// loss. The prompt text and the callback that runs on y live in
	// Model.confirm.
	ScreenConfirm
	// ScreenMCPManager is the full-screen modal for attaching /
	// detaching MCP servers to the currently focused session.
	// Payload lives in Model.mcpPanel.
	ScreenMCPManager
	// ScreenSkillManager is the full-screen modal for attaching /
	// detaching Claude Code skills. Payload lives in
	// Model.skillPanel.
	ScreenSkillManager
	// ScreenSettings is the read-only settings snapshot. It renders
	// the merged config and the config file paths.
	ScreenSettings
	// ScreenConductor is the Conductor Panel — a lazygit-style
	// split view of every persisted conductor + the tasks of the
	// selected one. Payload lives in Model.conductorPanel.
	ScreenConductor
	// ScreenConductorWizard is the multi-step wizard for creating
	// a new conductor plan from the TUI. Payload lives in
	// Model.conductorWizard.
	ScreenConductorWizard
	// ScreenDiff is the lazygit-style diff browser: file list on
	// the left, unified diff for the selected file on the right.
	// Payload lives in Model.diffPanel.
	ScreenDiff
	// ScreenFirstRun is the onboarding wizard shown the very
	// first time the user launches mosaic (detected by the
	// absence of ~/.mosaic/.first-run-done). Payload lives in
	// Model.firstRun.
	ScreenFirstRun
	// ScreenErrorLog is the full-screen error history (`E`
	// key). It reads from Model.errorLog (a ring buffer) and
	// lets the user scroll through recent errors / warnings.
	ScreenErrorLog
	// ScreenProfile is the profile switcher picker. Selecting
	// a profile quits the TUI and cmd_root restarts the
	// binary with --profile.
	ScreenProfile
)

// Model is the Bubble Tea state for the mosaic TUI.
type Model struct {
	// Dependencies (constructor-injected so the TUI is testable).
	sessions *service.SessionService
	store    *store.Store
	bus      *bus.Bus
	theme    *theme.Theme
	config   config.Config

	// Window dimensions.
	width  int
	height int

	// View state.
	screen Screen

	// rows is the flattened list currently rendered in the TOP
	// view. It is derived from lastSessions + subagents +
	// groupExpanded and rebuilt by [Model.rebuildRows] whenever
	// any of those inputs change.
	rows []displayRow

	// lastSessions is the authoritative list of sessions from the
	// most recent [service.SessionService.List] call. Derived
	// state (rows) is rebuilt from this — reading from rows leads
	// to duplicates because each sub-agent row carries its parent
	// Session value.
	lastSessions []store.Session

	// firstRefreshDone is flipped to true the first time an
	// asynchronous refresh returns. Before that, the TUI shows a
	// "loading..." placeholder instead of "no sessions yet", so
	// the user does not see a misleading empty state flash on
	// startup.
	firstRefreshDone bool

	subagents     map[string][]store.Subagent // sessionID → sub-agents
	cursor        int
	groupExpanded map[string]bool
	error         string
	statusMessage string
	keys          keyMap

	// capture holds the most recent tmux capture-pane output for
	// the session whose name matches captureSessionName. It is
	// refreshed once per tick for the currently-focused session
	// only, so the preview pane can show live Claude activity.
	capture            string
	captureSessionName string

	// Scroll-tickers etc.
	lastTick time.Time

	// Wizard state for the new-session modal.
	wizard wizardState

	// Confirmation overlay state. confirm.active is true while
	// the screen is showing; pressing y runs confirm.onConfirm
	// and closes the overlay, n (or Esc) just closes it.
	confirm confirmState

	// mcpPanel is the state behind the MCP Manager modal. It is
	// populated by [Model.openMCPManager] and consumed by
	// [Model.updateMCPPanel] / [Model.renderMCPPanel].
	mcpPanel mcpPanelState

	// skillPanel is the state behind the Skill Manager modal.
	skillPanel skillPanelState

	// configPaths carries the on-disk config locations the
	// Settings screen shows the user. It is optional: tests (which
	// do not care about paths) can leave it zero and the screen
	// just hides the "edit" hints.
	configPaths configPathsView

	// conductorPanel is the state behind the Conductor Panel
	// modal. See conductor_panel.go.
	conductorPanel conductorPanelState

	// conductorWizard is the state behind the conductor creation
	// wizard. See conductor_wizard.go.
	conductorWizard conductorWizardState

	// diffPanel is the state behind the Diff View modal.
	diffPanel diffPanelState

	// lastOSCSignal remembers, per mosaic session, the hash of
	// the OSC attention signal we most recently surfaced. It is
	// the suppression cache that makes OSC detection
	// edge-triggered: once a signal has been shown and later
	// cleared (by a hook, by the user, or by explicit dismiss),
	// the same signal sitting in the tmux visible buffer does
	// not re-fire on the next tick. A genuinely new OSC has a
	// different hash and breaks the suppression.
	lastOSCSignal map[string]string

	// busEvents is the bridge between the async bus dispatch
	// goroutine and the Bubble Tea update loop. A single
	// subscription writes a tiny struct here whenever a
	// UI-relevant event lands; a tea.Cmd reads from the channel
	// and turns reads into [busEventMsg] values, which in turn
	// trigger an immediate refresh. This brings status updates
	// (idle → running, attention flips) from up to 1s of tick
	// latency down to the bus dispatch latency — a few tens of
	// milliseconds in practice.
	busEvents      chan struct{}
	busUnsubscribe func()

	// selected is the set of session IDs currently marked via
	// Space (or visual mode). Batch operations — kill, move,
	// restart — act on this set when non-empty. Keyed by
	// session id so cursor movement does not lose selections.
	selected map[string]bool

	// visualAnchor is the row index where visual mode started.
	// When -1, visual mode is off. When set, cursor moves
	// continuously extend the selection to cover every row
	// between the anchor and the current cursor.
	visualAnchor int

	// profilePanel is the profile switcher picker payload.
	profilePanel profilePanelState

	// pendingProfile is the target profile name the user
	// selected from the picker. cmd_root reads this after the
	// Bubble Tea program exits; a non-empty value means
	// "restart mosaic with --profile=<value>".
	pendingProfile string

	// firstRun holds the onboarding wizard state while it is
	// active. It is populated by ShowFirstRunWizard (called
	// from cmd_root when the marker file is missing) and
	// consumed by updateFirstRunWizard / renderFirstRunWizard.
	firstRun firstRunState

	// statusFilter restricts the TOP list to a subset of
	// sessions. The header shows the active tab; `!` `@` `#`
	// `$` keys switch it, `0` returns to all.
	statusFilter statusFilterMode

	// errorLog is a ring buffer of the most recent error
	// entries for the E-key log screen. Errors are appended
	// by [Model.logError] and rendered in reverse order.
	errorLog []logEntry

	// topToast is a transient message line rendered at the
	// bottom of the TOP screen. Used for info / success /
	// warning messages that should grab the eye but not
	// block input.
	topToast        string
	topToastExpires time.Time

	// Settings editor state.
	settingsCursorIdx int
	settingsUnsaved   bool
	settingsError     string

	// spinnerFrame counts the cosmetic spinner ticks. It is
	// incremented by a 100ms tea.Tick that runs alongside the
	// main 1Hz refresh so loading states + the attention pulse
	// animate independently of the data refresh.
	spinnerFrame int

	// previewMode toggles between the structured preview, the
	// raw tmux capture, sub-agent timeline and quick diff
	// summary on the right pane. Cycled with Tab on the TOP
	// screen.
	previewMode previewMode
}

// previewMode enumerates the right-pane layouts the user can
// cycle through with Tab.
type previewMode int

const (
	previewStructured previewMode = iota
	previewLiveCapture
	previewSubAgentsOnly
	previewDiffSummary
)

func (p previewMode) label() string {
	switch p {
	case previewLiveCapture:
		return "live"
	case previewSubAgentsOnly:
		return "sub-agents"
	case previewDiffSummary:
		return "diff"
	default:
		return "structured"
	}
}

// PendingProfile returns the profile the user asked to switch
// to via the profile picker, or empty string when no switch
// was requested. cmd_root calls this after tea.Program.Run
// returns.
func (m *Model) PendingProfile() string {
	return m.pendingProfile
}

// version is the mosaic build version the TUI header displays.
// cmd_root sets it via [Model.SetVersion] right after NewModel
// because version is a cmd-scoped variable that internal/tui
// has no direct import on.
var version = "dev"

// SetVersion lets cmd_root share its build-time version string
// with the TUI so the header can show it next to the brand.
func (m *Model) SetVersion(v string) {
	if v == "" {
		return
	}
	version = v
}

// statusFilterMode represents the left-pane filter tab (`! @ # $` row).
type statusFilterMode int

const (
	statusFilterNone    statusFilterMode = iota
	statusFilterWaiting                  // `!`
	statusFilterRunning                  // `@`
	statusFilterIdle                     // `#`
	statusFilterError                    // `$`
)

func (f statusFilterMode) label() string {
	switch f {
	case statusFilterWaiting:
		return "waiting"
	case statusFilterRunning:
		return "running"
	case statusFilterIdle:
		return "idle"
	case statusFilterError:
		return "error"
	default:
		return "all"
	}
}

// logEntry is one row of the error log ring buffer.
type logEntry struct {
	Level   string // "info" | "warn" | "error"
	Title   string
	Body    string
	When    time.Time
}

// configPathsView is the subset of mosaic paths the Settings
// screen wants to display. It lives in the tui package so the
// deeper mosaicpath type does not leak into the model.
type configPathsView struct {
	Global   string
	Profile  string
	Database string
	LogFile  string
}

// SetConfigPaths records the on-disk locations the Settings modal
// reports. Commands call this right after [NewModel] so the
// snapshot knows where the user would go to edit each file.
func (m *Model) SetConfigPaths(global, profile, database, logFile string) {
	m.configPaths = configPathsView{
		Global:   global,
		Profile:  profile,
		Database: database,
		LogFile:  logFile,
	}
}

// confirmState is the generic "are you sure?" modal payload. It is
// reused by every destructive TUI action so each one does not need
// its own confirmation screen.
type confirmState struct {
	active    bool
	title     string
	body      string
	yesLabel  string
	onConfirm func() tea.Cmd
}

// displayRow is one renderable line in the TOP screen's left tree.
// A row represents either a project group header, a session, or a
// sub-agent indented under its parent session.
type displayRow struct {
	IsGroup       bool
	IsSubagent    bool
	GroupName     string
	GroupCount    int
	GroupExpanded bool

	// IsLastSibling marks the final sub-agent in a parent's
	// list. Sub-agent rendering uses this to pick the right
	// tree connector glyph: └─ for the last child, ├─ for
	// every other child.
	IsLastSibling bool

	Session  store.Session
	Subagent store.Subagent
}

// NewModel constructs a Model. The store enables sub-agent rendering;
// pass nil to skip the sub-agent fetch (the TUI will still work, the
// list just won't show indented children). The config drives the
// wizard's project registry, default tool/setup selections, and
// keymap overrides.
func NewModel(svc *service.SessionService, st *store.Store, b *bus.Bus, t *theme.Theme, cfg config.Config) *Model {
	if t == nil {
		t = theme.CatppuccinMocha()
	}
	keys := defaultKeys()
	keys.applyOverrides(cfg.UI.Keymap)
	m := &Model{
		sessions:      svc,
		store:         st,
		bus:           b,
		theme:         t,
		config:        cfg,
		groupExpanded: map[string]bool{},
		subagents:     map[string][]store.Subagent{},
		keys:          keys,
		busEvents:     make(chan struct{}, 16),
		selected:      map[string]bool{},
		visualAnchor:  -1,
	}
	// Subscribe to the in-process bus so backend state changes
	// (hook busy events, transcript entries, session lifecycle,
	// sub-agent spawns) can trigger an immediate TUI refresh
	// instead of waiting for the 1Hz tick.
	if b != nil {
		m.busUnsubscribe = b.Subscribe(func(e bus.Event) {
			if !isUIRelevantEvent(e) {
				return
			}
			// Non-blocking signal: drop when the channel is full
			// because a pending refresh is already queued — we
			// only need one trigger per refresh cycle.
			select {
			case m.busEvents <- struct{}{}:
			default:
			}
		})
	}
	return m
}

// isUIRelevantEvent returns true for bus events that plausibly
// change something the TUI cares about (session list, status
// icons, sub-agent rows, attention banner). Irrelevant events
// (internal bookkeeping, if any) get filtered here so the
// refresh loop stays cheap.
func isUIRelevantEvent(e bus.Event) bool {
	switch e.(type) {
	case bus.SessionStarted, bus.SessionStopped, bus.SessionStatus:
		return true
	}
	// All other known events (claudeio transcript entries / sub-
	// agent spawns, hook busy / notification events) should trigger
	// a refresh too. We use a nominal type assertion rather than
	// importing every producer package — the type switch stays in
	// sync with new event types via the default case, which keeps
	// latency low at the cost of some false-positive refreshes.
	return true
}

// --- Bubble Tea contract -----------------------------------------------------

// Init kicks off the first refresh, the periodic ticker, and the
// bus-event reader loop.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshCmd(),
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return spinnerTickMsg(t) }),
		m.waitForBusEvent(),
	)
}

// spinnerTickMsg is the cosmetic-animation tick. It runs at
// ~8Hz so the loading spinner rotates and the attention pulse
// fades smoothly without the heavy data-refresh price tag.
type spinnerTickMsg time.Time

// busEventMsg signals that a UI-relevant bus event has landed.
// The handler in Update reacts by triggering an immediate refresh
// AND re-issuing the wait so the loop continues.
type busEventMsg struct{}

// waitForBusEvent returns a tea.Cmd that blocks on the next bus
// signal and emits a busEventMsg. It is the read side of the
// busEvents channel; producers (the bus subscriber) write to the
// channel and this reader pulls them out one at a time.
func (m *Model) waitForBusEvent() tea.Cmd {
	if m.busEvents == nil {
		return nil
	}
	return func() tea.Msg {
		<-m.busEvents
		return busEventMsg{}
	}
}

// Update handles input and refresh messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height

	case tea.KeyMsg:
		return m.handleKey(v)

	case refreshedMsg:
		m.firstRefreshDone = true
		if v.subagents != nil {
			m.subagents = v.subagents
		}
		// Compare the pre/post attention state of every session
		// so hook-sourced attention flips (which happen in the
		// reconciler, not in applyAttentionHint) still produce a
		// native OS notification. The previous state comes from
		// m.lastSessions before we call applySessions.
		m.fireAttentionDiffs(v.list.Sessions)
		m.applySessions(v.list.Sessions, v.list.TmuxMissing)
		// Only adopt the capture when it belongs to the currently
		// focused session; a race between cursor moves and an
		// in-flight refresh could otherwise show output from the
		// wrong session.
		if v.captureSessionName != "" && v.captureSessionName == m.focusedSessionName() {
			m.capture = v.capture
			m.captureSessionName = v.captureSessionName
		}
		if v.err != nil {
			m.flagError("refresh failed", v.err.Error())
		} else {
			m.error = ""
		}

	case tickMsg:
		// Re-fetch sessions on every tick so the live LAST_ACTIVE
		// counter and reconcile updates show up. The bus
		// subscription handles instant updates between ticks; the
		// tick is here as a safety net for clock-only changes
		// (elapsed timers, stale capture refresh).
		return m, tea.Batch(
			m.refreshCmd(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	case spinnerTickMsg:
		m.spinnerFrame++
		// Re-arm the cosmetic-only tick. We don't trigger a
		// data refresh here — only a re-render via the
		// natural Bubble Tea cycle that follows Update.
		return m, tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
			return spinnerTickMsg(t)
		})

	case busEventMsg:
		// Backend event landed → refresh now AND re-arm the
		// reader so the loop keeps going. We coalesce by
		// returning a single refreshCmd; if many events land
		// before the refresh completes, the reader's channel
		// drops them (the buffered channel in NewModel) and
		// the next refresh picks up whatever the latest state
		// is. That gives us "fast enough" without thrashing
		// the store.
		return m, tea.Batch(
			m.refreshCmd(),
			m.waitForBusEvent(),
		)

	case diffPanelRefreshedMsg:
		// Preserve the previously-cached per-file diffs so a
		// refresh does not throw away work the user paid for.
		old := m.diffPanel
		m.diffPanel = v.state
		for path, body := range old.fileDiffs {
			if _, exists := m.diffPanel.fileDiffs[path]; !exists {
				m.diffPanel.fileDiffs[path] = body
			}
		}
		m.ensureDiffLoaded()
		return m, nil

	case diffPanelToastMsg:
		m.setDiffToast(v.text)
		return m, nil

	case diffPanelCommitResultMsg:
		if v.success {
			m.diffPanel.mode = diffModeBrowse
			m.diffPanel.commit = diffCommitState{}
			m.setDiffToast("commit created")
			return m, m.refreshDiffPanel()
		}
		m.diffPanel.commit.err = v.err
		return m, nil

	case conductorWizardCompleteMsg:
		if v.err != nil {
			m.conductorWizard.validationErr = v.err.Error()
			return m, nil
		}
		// Wizard succeeded — switch to the conductor panel and
		// show the new conductor in the list.
		m.openConductorPanel()
		m.setConductorToast("running " + v.name)
		return m, nil

	case conductorPanelRefreshMsg:
		if v.toast != "" {
			m.setConductorToast(v.toast)
		}
		// Re-fetch the panel data so the mutated row renders
		// with its new status without waiting for the user to
		// press R.
		m.openConductorPanel()
		return m, nil

	case conductorPanelToastMsg:
		m.setConductorToast(v.text)
		return m, nil

	case conductorPanelSendSuccessMsg:
		m.conductorPanel.sendModal = conductorSendModal{}
		m.setConductorToast("sent to " + v.target)
		return m, nil

	case forkResultMsg:
		if v.success {
			m.setTopToast("forked " + v.source)
			return m, m.refreshCmd()
		}
		m.flagError("fork failed", v.err)
		return m, nil

	case respawnResultMsg:
		if len(v.failed) > 0 {
			m.setTopToast(fmt.Sprintf("respawned %d/%d (%d failed)", v.success, v.total, len(v.failed)))
			for _, f := range v.failed {
				m.flagError("respawn failed", f)
			}
		} else {
			m.setTopToast(fmt.Sprintf("respawned %d session(s)", v.success))
		}
		m.selected = map[string]bool{}
		return m, m.refreshCmd()

	case dismissToastMsg:
		m.setTopToast("dismissed " + v.name)
		return m, m.refreshCmd()

	case settingsSaveResult:
		if v.success {
			m.settingsUnsaved = false
			m.settingsError = ""
			m.setTopToast("config saved")
		} else {
			m.settingsError = v.err
		}
		return m, nil
	}
	return m, nil
}

// View dispatches to the screen-specific render function.
func (m *Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	// The confirmation overlay floats on top of whatever screen
	// spawned it. We render the TOP beneath and overlay the
	// confirm modal; Bubble Tea has no native layering so the
	// modal replaces the body.
	if m.confirm.active {
		return m.renderConfirm()
	}
	switch m.screen {
	case ScreenHelp:
		return m.renderHelp()
	case ScreenNewSession:
		return m.renderWizard()
	case ScreenMCPManager:
		return m.renderMCPPanel()
	case ScreenSkillManager:
		return m.renderSkillPanel()
	case ScreenSettings:
		return m.renderSettings()
	case ScreenConductor:
		return m.renderConductorPanel()
	case ScreenConductorWizard:
		return m.renderConductorWizard()
	case ScreenDiff:
		return m.renderDiffPanel()
	case ScreenFirstRun:
		return m.renderFirstRunWizard()
	case ScreenErrorLog:
		return m.renderErrorLog()
	case ScreenProfile:
		return m.renderProfilePanel()
	default:
		return m.renderTop()
	}
}

// --- Internal helpers --------------------------------------------------------

// refreshedMsg is sent by [Model.refreshCmd] when a refresh completes.
type refreshedMsg struct {
	list      service.ListResult
	subagents map[string][]store.Subagent
	// capture carries the tmux capture-pane output of the
	// currently-focused session, plus the name it belongs to so
	// the Model can ignore stale captures that land after the
	// cursor has moved.
	capture            string
	captureSessionName string
	err                error
}

// tickMsg is the periodic refresh signal.
type tickMsg time.Time

// refreshCmd returns a tea.Cmd that fetches the current session list
// (and any associated sub-agents) and packages the result into a
// [refreshedMsg]. It is used by the periodic tick and by any action
// (kill / attach / create) that wants the TUI to reflect a state
// change immediately.
func (m *Model) refreshCmd() tea.Cmd {
	if m.sessions == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return m.fetchRefreshed(ctx)
	}
}

// fetchRefreshed performs a single refresh round-trip against the
// store and returns a fully-populated [refreshedMsg]. It is shared
// between the tick loop and the action handlers — the latter call
// it synchronously inside their own tea.Cmd goroutine so the TUI
// never flashes an empty list between "action started" and "next
// tick arrived".
//
// The tick path also asks for a tmux capture-pane snapshot of every
// session and scans it for interactive permission prompts. When one
// is detected, the session's attention_state is flipped to
// waiting_input so the TUI can highlight it immediately — this is
// the "which agent needs me right now?" killer feature working
// without requiring the user to install any Claude Code hooks.
func (m *Model) fetchRefreshed(ctx context.Context) refreshedMsg {
	res, err := m.sessions.List(ctx)
	subs := map[string][]store.Subagent{}
	if m.store != nil && err == nil {
		for _, s := range res.Sessions {
			rows, lerr := m.store.Subagents().ListByParent(ctx, s.ID)
			if lerr != nil {
				continue
			}
			if len(rows) > 0 {
				subs[s.ID] = rows
			}
		}
	}

	// Scan every running session's pane for interactive prompts.
	// Capturing idle / stopped sessions is wasted work, so we
	// skip them.
	if err == nil && m.store != nil {
		for _, s := range res.Sessions {
			if s.Status == string(store.StatusStopped) {
				continue
			}
			out, cerr := m.sessions.CapturePane(ctx, s.Name, 80)
			if cerr != nil || out == "" {
				continue
			}
			m.applyAttentionHint(ctx, s, out)
		}
	}

	var capture, captureName string
	if focusedName := m.focusedSessionName(); focusedName != "" {
		out, cerr := m.sessions.CapturePane(ctx, focusedName, 80)
		if cerr == nil {
			capture = out
			captureName = focusedName
		}
	}

	// After the potential attention flips, re-read the session
	// list so the returned snapshot reflects the latest state
	// (the list above was fetched BEFORE the attention writes).
	refreshed := res
	if err == nil {
		if updated, lerr := m.sessions.List(ctx); lerr == nil {
			refreshed = updated
		}
	}

	return refreshedMsg{
		list:               refreshed,
		subagents:          subs,
		capture:            capture,
		captureSessionName: captureName,
		err:                err,
	}
}

// fireAttentionDiffs compares the incoming session snapshot
// against the previous cached one and fires an OS notification
// for every session that just transitioned into
// AttentionWaitingInput. The hook-sourced flip lands here because
// applyAttentionHint is capture-only; without this diff walk,
// installing Claude Code hooks would silently bypass the
// notification path.
func (m *Model) fireAttentionDiffs(next []store.Session) {
	if !m.config.UI.Attention.NotifyOS && !m.config.UI.Attention.Sound {
		return
	}
	prev := map[string]string{}
	for _, s := range m.lastSessions {
		prev[s.ID] = s.AttentionState
	}
	for _, s := range next {
		if s.AttentionState != string(store.AttentionWaitingInput) {
			continue
		}
		if prev[s.ID] == string(store.AttentionWaitingInput) {
			continue // already waiting, not a fresh transition
		}
		title := s.AttentionTitle.String
		body := s.AttentionBody.String
		m.fireAttentionNotification(s.Name, title, body)
	}
}

// setTopToast shows a short message in the TOP status bar that
// fades after a couple of seconds. Used for dismiss / filter /
// fork confirmations without taking over the screen.
func (m *Model) setTopToast(text string) {
	m.topToast = text
	m.topToastExpires = time.Now().Add(3 * time.Second)
}

// logError appends an error entry to the ring buffer that backs
// the E-key error log screen. The buffer is capped at 100 so a
// very noisy session cannot grow the Model without bound.
func (m *Model) logError(level, title, body string) {
	const maxLog = 100
	entry := logEntry{
		Level: level,
		Title: title,
		Body:  body,
		When:  time.Now(),
	}
	m.errorLog = append(m.errorLog, entry)
	if n := len(m.errorLog); n > maxLog {
		m.errorLog = m.errorLog[n-maxLog:]
	}
}

// flagError records an error in the log AND sets the status-bar
// error hint AND shows a transient toast — the three surfaces a
// mosaic user has for noticing something went wrong. Callers
// that want only the log (silent background failures) should
// use logError directly.
func (m *Model) flagError(title, body string) {
	m.logError("error", title, body)
	m.error = title
	m.setTopToast(title + " (E for log)")
}

// fireAttentionNotification dispatches one attention notification
// according to the user's [ui.attention] preferences. Both
// notify_os and sound are independent: neither, one, or both may
// fire for a given transition.
func (m *Model) fireAttentionNotification(sessionName, title, body string) {
	if m.config.UI.Attention.NotifyOS {
		t := title
		if t == "" {
			t = "mosaic: " + sessionName
		} else {
			t = "mosaic: " + sessionName + " — " + t
		}
		go notifyOS(t, body)
	}
	if m.config.UI.Attention.Sound {
		go playSound(m.config.UI.Attention.SoundFile)
	}
}

// focusedSessionName returns the name of the session currently
// highlighted in the left tree — or empty when the cursor is on a
// group header or a sub-agent row, or when nothing is loaded yet.
//
// Called on the tick-handler goroutine (no lock needed because
// Bubble Tea serialises Model access around Update()).
func (m *Model) focusedSessionName() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	row := m.rows[m.cursor]
	if row.IsGroup {
		return ""
	}
	return row.Session.Name
}

// applyAttentionHint detects interactive permission prompts in a
// tmux capture and mirrors the result into the sessions table.
// Sessions without a prompt have their previously-set "capture" or
// "osc" attention cleared so the ⚠ icon disappears the moment the
// user chooses an option.
//
// Two detectors run against the capture:
//
//  1. [claudeio.DetectAttention] recognises plain-text permission
//     prompts emitted by Claude Code's TUI ("Do you want to
//     proceed?" and friends). These dominate today.
//  2. [claudeio.ScanOSC] + [claudeio.OscAttention] recognise
//     structured OSC 777 / OSC 9;4 escapes so external tools
//     (custom scripts, codex, gemini, ...) can surface attention
//     without mosaic having to know about their prompt text.
//
// When both detectors fire, the plain-text prompt wins: it is much
// higher confidence and carries a human-readable body. OSC is the
// fallback for the tools mosaic does not natively understand.
func (m *Model) applyAttentionHint(ctx context.Context, s store.Session, capture string) {
	hint := claudeio.DetectAttention(capture)
	if !hint.Waiting {
		if osc := claudeio.OscAttention(claudeio.ScanOSC(capture)); osc.Waiting {
			hint = osc
		}
	}
	currentlyWaiting := s.AttentionState == string(store.AttentionWaitingInput)
	currentSource := s.AttentionSource.String

	// OSC suppression: tmux capture-pane returns the visible
	// buffer each tick, so an OSC sequence Claude Code once
	// echoed into the pane keeps showing up in every subsequent
	// snapshot. Without suppression we would re-set attention on
	// every tick after a hook (or a manual dismiss) cleared it.
	//
	// The rule is: if we have already shown this exact OSC to
	// the user AND something cleared it, stay silent until a
	// *different* OSC appears. A matching hash while we are
	// currently waiting is the normal "keep" case and falls
	// through to the switch below.
	if hint.Waiting && hint.Source == "osc" {
		sig := hint.Title + "\x00" + hint.Body
		if m.lastOSCSignal[s.ID] == sig && !currentlyWaiting {
			return
		}
		if m.lastOSCSignal == nil {
			m.lastOSCSignal = map[string]string{}
		}
		m.lastOSCSignal[s.ID] = sig
	}

	switch {
	case hint.Waiting && !currentlyWaiting:
		// Transition idle → waiting.
		_ = m.store.Sessions().SetAttention(ctx, s.ID,
			store.AttentionWaitingInput, hint.Title, hint.Body, hint.Source)
		m.fireAttentionNotification(s.Name, hint.Title, hint.Body)

	case hint.Waiting && currentlyWaiting:
		// Keep the existing attention. Update the body only when
		// we have a better one so the banner stays in sync with
		// what the tool is currently asking.
		if hint.Body != "" && hint.Body != s.AttentionBody.String {
			_ = m.store.Sessions().SetAttention(ctx, s.ID,
				store.AttentionWaitingInput, hint.Title, hint.Body, hint.Source)
		}

	case !hint.Waiting && currentlyWaiting && (currentSource == "capture" || currentSource == "osc"):
		// Previously detected from capture or OSC, now gone → clear
		// it. We only clear captures we set ourselves so a
		// hook-sourced attention is not wiped by a stale capture
		// tick.
		_ = m.store.Sessions().SetAttention(ctx, s.ID,
			store.AttentionNone, "", "", "")
		// The OSC vanished from the pane — drop the suppression
		// entry so if it reappears later it will fire again.
		delete(m.lastOSCSignal, s.ID)
	}
}

// applySessions stores the canonical session list and then rebuilds
// the rendered rows. Callers should only ever pass the result of a
// fresh refresh — never a slice derived from [Model.rows], because
// that list contains sub-agent entries whose .Session field
// duplicates the parent.
func (m *Model) applySessions(sessions []store.Session, missing []string) {
	m.lastSessions = append(m.lastSessions[:0], sessions...)
	m.rebuildRows()
	if len(missing) > 0 {
		m.setTopToast(fmt.Sprintf(
			"%d session(s) stopped (tmux gone) — r to respawn", len(missing)))
	}
}

// rebuildRows re-computes [Model.rows] from the cached session list
// plus the current expansion map. It is the only function that
// touches m.rows, so every TUI interaction that changes group
// state (collapse, expand, subagent update, tick refresh) funnels
// through here.
func (m *Model) rebuildRows() {
	groups := map[string][]store.Session{}
	for _, s := range m.lastSessions {
		if !m.matchesStatusFilter(s) {
			continue
		}
		groups[m.groupNameFor(s)] = append(groups[m.groupNameFor(s)], s)
	}

	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	// Default every group to expanded the first time we see it.
	for _, name := range groupNames {
		if _, seen := m.groupExpanded[name]; !seen {
			m.groupExpanded[name] = true
		}
	}

	var rows []displayRow
	for _, name := range groupNames {
		expanded := m.groupExpanded[name]
		rows = append(rows, displayRow{
			IsGroup:       true,
			GroupName:     name,
			GroupCount:    len(groups[name]),
			GroupExpanded: expanded,
		})
		if !expanded {
			continue
		}
		// Sort sessions inside a group: attention → error → running → idle
		grp := append([]store.Session{}, groups[name]...)
		sort.SliceStable(grp, func(i, j int) bool {
			ri, rj := statusRank(grp[i]), statusRank(grp[j])
			if ri != rj {
				return ri < rj
			}
			return grp[i].Name < grp[j].Name
		})
		for _, s := range grp {
			rows = append(rows, displayRow{Session: s})
			// Inline sub-agent rows beneath the parent. The
			// final entry is tagged so the renderer can swap
			// ├─ for └─ and the connectors line up cleanly.
			subs := m.subagents[s.ID]
			for i, sub := range subs {
				rows = append(rows, displayRow{
					IsSubagent:    true,
					IsLastSibling: i == len(subs)-1,
					Session:       s,
					Subagent:      sub,
				})
			}
		}
	}

	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// matchesStatusFilter returns true when the session belongs in
// the TOP list under the current filter tab. `statusFilterNone`
// matches everything, a specific tab matches its own bucket.
// Attention (waiting_input) always wins over the underlying
// status column so a "running + waiting" session shows up under
// `!` rather than `@`.
func (m *Model) matchesStatusFilter(s store.Session) bool {
	status := s.Status
	if s.AttentionState == string(store.AttentionWaitingInput) {
		status = "waiting"
	}
	switch m.statusFilter {
	case statusFilterWaiting:
		return status == "waiting"
	case statusFilterRunning:
		return status == string(store.StatusRunning)
	case statusFilterIdle:
		return status == string(store.StatusIdle) ||
			status == string(store.StatusStopped)
	case statusFilterError:
		return status == "error" ||
			s.AttentionState == string(store.AttentionError)
	default:
		return true
	}
}

// groupNameFor decides which project bucket a session belongs to. We
// use the basename of the project path; sessions whose path is empty
// fall into "my-sessions".
func (m *Model) groupNameFor(s store.Session) string {
	if s.ProjectPath == "" {
		return "my-sessions"
	}
	return baseName(s.ProjectPath)
}

// statusRank orders sessions inside a group: attention first, then
// error, then running, then idle/stopped.
func statusRank(s store.Session) int {
	switch {
	case s.AttentionState == string(store.AttentionWaitingInput):
		return 0
	case s.AttentionState == string(store.AttentionError),
		s.Status == string(store.StatusError):
		return 1
	case s.Status == string(store.StatusRunning),
		s.Status == string(store.StatusWaiting):
		return 2
	default:
		return 3
	}
}

// baseName is filepath.Base without dragging in the os/filepath dep
// just for one call.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
