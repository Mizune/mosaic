package hook

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/Mizune/mosaic/internal/bus"
)

// SocketDir returns /tmp/mosaic-<uid>, which is the parent directory
// every mosaic process puts its hook socket under. Per-process
// sockets sit at <SocketDir>/pid-<pid>.
func SocketDir() string {
	uid := os.Getuid()
	return filepath.Join("/tmp", fmt.Sprintf("mosaic-%d", uid))
}

// SocketPath returns the per-pid socket path for the current process.
func SocketPath() string {
	return filepath.Join(SocketDir(), fmt.Sprintf("pid-%d", os.Getpid()))
}

// Server listens on a unix socket and republishes hook payloads on a
// [bus.Bus]. The on-the-wire format is supacode-compatible:
//
//	<worktreeID> <tabID> <surfaceID> <0|1>\n
//	  → BusyEvent
//
//	<worktreeID> <tabID> <surfaceID> <agent>\n<JSON payload>\n
//	  → NotificationEvent
type Server struct {
	bus *bus.Bus
	log *slog.Logger

	mu       sync.Mutex
	listener net.Listener
	cancel   context.CancelFunc
}

// BusyEvent is published when a hook reports the agent's busy flag.
type BusyEvent struct {
	WorktreeID string
	TabID      string
	SurfaceID  string
	Active     bool
}

// NotificationEvent is published when a hook forwards a JSON payload.
type NotificationEvent struct {
	WorktreeID string
	TabID      string
	SurfaceID  string
	Agent      string
	Title      string
	Body       string
	Event      string
}

// NewServer constructs a Server.
func NewServer(b *bus.Bus) *Server {
	return &Server{
		bus: b,
		log: slog.With("subsys", "hook.server"),
	}
}

// Listen creates the per-pid socket and starts accepting connections
// in a background goroutine. The returned cleanup function shuts the
// server down and removes the socket file.
func (s *Server) Listen(ctx context.Context) (string, error) {
	if err := s.prepareSocketDir(); err != nil {
		return "", err
	}
	path := SocketPath()
	_ = os.Remove(path)

	l, err := net.Listen("unix", path)
	if err != nil {
		return "", fmt.Errorf("listen %s: %w", path, err)
	}

	s.mu.Lock()
	s.listener = l
	cctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go s.acceptLoop(cctx, l)
	return path, nil
}

// Close stops the listener and removes the socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.listener != nil {
		path := s.listener.Addr().String()
		_ = s.listener.Close()
		_ = os.Remove(path)
		s.listener = nil
	}
	return nil
}

// prepareSocketDir creates the per-uid directory with 0700 perms and
// removes any stale per-pid sockets whose process is no longer alive.
func (s *Server) prepareSocketDir() error {
	dir := SocketDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "pid-") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(name, "pid-"))
		if err != nil {
			continue
		}
		if isProcessAlive(pid) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Signal 0 only checks for the existence/permission of the
	// process, it never delivers anything.
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (s *Server) acceptLoop(ctx context.Context, l net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			s.log.Warn("accept", "err", err)
			continue
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	body, err := io.ReadAll(io.LimitReader(conn, 64*1024))
	if err != nil {
		s.log.Warn("read", "err", err)
		return
	}
	if len(body) == 0 {
		return
	}
	if msg, ok := parseMessage(body); ok {
		s.bus.Publish(msg)
	}
}

// parseMessage decodes one wire message into either a [BusyEvent] or
// [NotificationEvent]. Unknown shapes return (nil, false).
func parseMessage(body []byte) (any, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	if !scanner.Scan() {
		return nil, false
	}
	header := scanner.Text()
	parts := strings.SplitN(header, " ", 4)
	if len(parts) < 3 {
		return nil, false
	}
	worktree, tab, surface := parts[0], parts[1], parts[2]
	if len(parts) == 4 {
		// Could be busy flag (single char "0" or "1") OR notification
		// header (agent name).
		fourth := parts[3]
		if fourth == "0" || fourth == "1" {
			return BusyEvent{
				WorktreeID: worktree, TabID: tab, SurfaceID: surface,
				Active: fourth == "1",
			}, true
		}
		// Notification: read remaining body as JSON payload.
		var jsonBody strings.Builder
		for scanner.Scan() {
			jsonBody.WriteString(scanner.Text())
			jsonBody.WriteByte('\n')
		}
		var payload notificationPayload
		_ = json.Unmarshal([]byte(jsonBody.String()), &payload)
		return NotificationEvent{
			WorktreeID: worktree,
			TabID:      tab,
			SurfaceID:  surface,
			Agent:      fourth,
			Title:      payload.Title,
			Body:       firstNonEmpty(payload.Message, payload.LastAssistantMessage),
			Event:      payload.HookEventName,
		}, true
	}
	return nil, false
}

// notificationPayload mirrors the supacode-compatible Claude Code
// hook payload schema.
type notificationPayload struct {
	HookEventName        string `json:"hook_event_name"`
	Title                string `json:"title"`
	Message              string `json:"message"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
