// Package hook installs, removes and inspects mosaic-owned entries in
// ~/.claude/settings.json, and runs the unix-socket server that
// receives hook payloads from Claude Code at runtime.
//
// The settings.json schema mosaic targets is the same one supacode
// uses (see https://github.com/supabitapp/supacode):
//
//	{
//	  "hooks": {
//	    "<EventName>": [
//	      {
//	        "matcher": "<optional>",
//	        "hooks": [
//	          { "type": "command", "command": "...", "async": true }
//	        ]
//	      }
//	    ]
//	  }
//	}
//
// mosaic-owned commands are identified by the presence of the marker
// environment variable name "MOSAIC_SOCKET_PATH" inside the command
// string. That makes uninstall idempotent and means we never remove
// hooks the user (or another tool) added.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MarkerEnvVar is the environment variable name embedded in every
// mosaic-installed hook command. The installer/uninstaller use this
// substring to recognise commands that belong to mosaic.
const MarkerEnvVar = "MOSAIC_SOCKET_PATH"

// Command builds the shell command string mosaic injects into a hook
// entry. The command:
//
//   - exits 0 (silently) when the marker env vars are not set, so it
//     is safe to leave installed when mosaic is not running;
//   - sends the worktree/tab/surface ids plus a 1/0 busy flag (or a
//     JSON payload from stdin) to mosaic's unix socket via `nc -U`,
//     which is shipped on every supported platform.
func BusyCommand(active bool) string {
	flag := "0"
	if active {
		flag = "1"
	}
	return mustEnvCheck() + ` && echo "$MOSAIC_WORKTREE_ID $MOSAIC_TAB_ID $MOSAIC_SURFACE_ID ` + flag + `" | /usr/bin/nc -U -w1 "$MOSAIC_SOCKET_PATH" 2>/dev/null || true`
}

// NotificationCommand builds the variant that forwards the hook event
// payload (read from stdin) to mosaic with a header line carrying the
// agent name.
func NotificationCommand(agent string) string {
	return mustEnvCheck() + ` && { printf '%s ` + agent + `\n' "$MOSAIC_WORKTREE_ID $MOSAIC_TAB_ID $MOSAIC_SURFACE_ID"; cat; } | /usr/bin/nc -U -w1 "$MOSAIC_SOCKET_PATH" 2>/dev/null || true`
}

// mustEnvCheck is the env-presence guard prepended to every mosaic
// command so the hook is a no-op when mosaic is not running.
func mustEnvCheck() string {
	return `[ -n "${MOSAIC_SOCKET_PATH:-}" ] && [ -n "${MOSAIC_WORKTREE_ID:-}" ] && [ -n "${MOSAIC_TAB_ID:-}" ] && [ -n "${MOSAIC_SURFACE_ID:-}" ]`
}

// EventName identifies which Claude Code hook event a command is
// installed under.
type EventName string

const (
	EventUserPromptSubmit   EventName = "UserPromptSubmit"
	EventStop               EventName = "Stop"
	EventPostToolUseFailure EventName = "PostToolUseFailure"
	EventSessionStart       EventName = "SessionStart"
	EventSessionEnd         EventName = "SessionEnd"
	EventNotification       EventName = "Notification"
	EventPermissionRequest  EventName = "PermissionRequest"
	EventPreToolUse         EventName = "PreToolUse"
	EventPostToolUse        EventName = "PostToolUse"
)

// MosaicHookSet is the canonical set of hooks mosaic installs. Tests
// and the installer call this to know what to write.
func MosaicHookSet() map[EventName]string {
	return map[EventName]string{
		EventUserPromptSubmit:   BusyCommand(true),
		EventStop:               BusyCommand(false),
		EventPostToolUseFailure: BusyCommand(false),
		EventSessionEnd:         BusyCommand(false),
		EventNotification:       NotificationCommand("claude"),
	}
}

// CodexHookSet is the canonical hook set for Codex CLI hooks.json.
func CodexHookSet() map[EventName]string {
	return map[EventName]string{
		EventUserPromptSubmit:  BusyCommand(true),
		EventPreToolUse:        BusyCommand(true),
		EventPostToolUse:       BusyCommand(false),
		EventStop:              BusyCommand(false),
		EventPermissionRequest: NotificationCommand("codex"),
	}
}

// Installer is the entry point for managing mosaic's settings.json
// presence.
type Installer struct {
	// Path is the absolute path to ~/.claude/settings.json. Tests
	// override this so they can use a fixture path under t.TempDir().
	Path string
}

// NewInstaller returns an Installer that targets the given path.
func NewInstaller(path string) *Installer {
	return &Installer{Path: path}
}

// IsInstalled reports whether at least one mosaic-owned command is
// present in the file. Returns false (no error) when the file does
// not exist.
func (i *Installer) IsInstalled() (bool, error) {
	doc, err := i.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	hooks := doc.hooks()
	for _, groups := range hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, MarkerEnvVar) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Install merges the mosaic hook set into settings.json. It is
// idempotent: re-running it after a successful install does not
// duplicate entries.
func (i *Installer) Install() error {
	return i.InstallSet(MosaicHookSet())
}

// InstallSet merges a caller-supplied hook set into the target config.
func (i *Installer) InstallSet(set map[EventName]string) error {
	return i.installSet(set, true, false)
}

// InstallSetSync installs hook commands without the Claude-only async
// field. Codex currently warns and skips async hooks, so its installer
// path uses this variant and replaces any older mosaic-owned async
// entries in the target file.
func (i *Installer) InstallSetSync(set map[EventName]string) error {
	return i.installSet(set, false, true)
}

func (i *Installer) installSet(set map[EventName]string, async, replace bool) error {
	doc, err := i.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if doc == nil {
		doc = newDoc()
	}
	if replace {
		doc.stripMosaic()
	}

	for event, command := range set {
		doc.addCommand(string(event), command, async)
	}
	return i.save(doc)
}

// Uninstall strips every mosaic-owned command from settings.json,
// leaving other entries untouched. Empty event arrays are removed.
func (i *Installer) Uninstall() error {
	doc, err := i.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	doc.stripMosaic()
	return i.save(doc)
}

// --- internal document model ------------------------------------------------

type doc struct {
	raw map[string]any
}

func newDoc() *doc {
	return &doc{raw: map[string]any{}}
}

type hookGroup struct {
	Matcher string  `json:"matcher,omitempty"`
	Hooks   []entry `json:"hooks"`
}

type entry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async,omitempty"`
}

// hooks returns the parsed view of doc.raw["hooks"].
func (d *doc) hooks() map[string][]hookGroup {
	out := map[string][]hookGroup{}
	rawHooks, ok := d.raw["hooks"].(map[string]any)
	if !ok {
		return out
	}
	for event, val := range rawHooks {
		groups, ok := val.([]any)
		if !ok {
			continue
		}
		for _, g := range groups {
			parsed, ok := g.(map[string]any)
			if !ok {
				continue
			}
			out[event] = append(out[event], parseGroup(parsed))
		}
	}
	return out
}

func parseGroup(raw map[string]any) hookGroup {
	var g hookGroup
	if m, ok := raw["matcher"].(string); ok {
		g.Matcher = m
	}
	if hs, ok := raw["hooks"].([]any); ok {
		for _, h := range hs {
			obj, ok := h.(map[string]any)
			if !ok {
				continue
			}
			var e entry
			if t, ok := obj["type"].(string); ok {
				e.Type = t
			}
			if c, ok := obj["command"].(string); ok {
				e.Command = c
			}
			if a, ok := obj["async"].(bool); ok {
				e.Async = a
			}
			g.Hooks = append(g.Hooks, e)
		}
	}
	return g
}

// addCommand inserts (or no-ops) a mosaic command on the given event.
// If a mosaic-owned command already exists, the call is a no-op so
// repeated installs are idempotent.
func (d *doc) addCommand(event, command string, async bool) {
	hooks, ok := d.raw["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		d.raw["hooks"] = hooks
	}
	groups, _ := hooks[event].([]any)

	// Idempotency check.
	for _, g := range groups {
		if obj, ok := g.(map[string]any); ok {
			if hs, ok := obj["hooks"].([]any); ok {
				for _, h := range hs {
					if hObj, ok := h.(map[string]any); ok {
						if c, _ := hObj["command"].(string); c == command {
							return
						}
					}
				}
			}
		}
	}

	entry := map[string]any{
		"type":    "command",
		"command": command,
	}
	if async {
		entry["async"] = true
	}
	groups = append(groups, map[string]any{"hooks": []any{entry}})
	hooks[event] = groups
}

// stripMosaic removes every mosaic-owned command from the document.
func (d *doc) stripMosaic() {
	hooks, ok := d.raw["hooks"].(map[string]any)
	if !ok {
		return
	}
	for event, val := range hooks {
		groups, ok := val.([]any)
		if !ok {
			continue
		}
		var keptGroups []any
		for _, g := range groups {
			obj, ok := g.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			hs, ok := obj["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			var keptHooks []any
			for _, h := range hs {
				if hObj, ok := h.(map[string]any); ok {
					if c, _ := hObj["command"].(string); strings.Contains(c, MarkerEnvVar) {
						continue
					}
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) > 0 {
				obj["hooks"] = keptHooks
				keptGroups = append(keptGroups, obj)
			}
		}
		if len(keptGroups) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keptGroups
		}
	}
	if len(hooks) == 0 {
		delete(d.raw, "hooks")
	}
}

func (i *Installer) load() (*doc, error) {
	body, err := os.ReadFile(i.Path)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return newDoc(), nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return &doc{raw: raw}, nil
}

func (i *Installer) save(d *doc) error {
	if err := os.MkdirAll(filepath.Dir(i.Path), 0o755); err != nil {
		return err
	}
	// Marshal with sorted keys for stable diffs.
	body, err := json.MarshalIndent(sortedMap(d.raw), "", "  ")
	if err != nil {
		return err
	}
	tmp := i.Path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, i.Path)
}

// sortedMap recursively returns a map[string]any whose iteration
// order is alphabetic. encoding/json already sorts keys for plain
// map[string]any, so this is mostly defensive against future changes.
func sortedMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := in[k].(type) {
		case map[string]any:
			out[k] = sortedMap(v)
		default:
			out[k] = v
		}
	}
	return out
}
