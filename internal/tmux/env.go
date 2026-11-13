package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// Env is the parsed value of the TMUX environment variable, which tmux
// exports into every pane as "socket-path,server-pid,session-index".
type Env struct {
	Socket       string
	PID          int
	SessionIndex int
}

// ParseEnv parses a TMUX value. It rejects an empty value and any shape
// tmux would not produce, so callers can treat an error as "not inside
// tmux".
func ParseEnv(v string) (Env, error) {
	parts := strings.Split(strings.TrimSpace(v), ",")
	if len(parts) != 3 || parts[0] == "" {
		return Env{}, fmt.Errorf("tmux: unexpected TMUX value %q", v)
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 {
		return Env{}, fmt.Errorf("tmux: unexpected server pid in TMUX value %q", v)
	}
	idx, err := strconv.Atoi(parts[2])
	if err != nil || idx < 0 {
		return Env{}, fmt.Errorf("tmux: unexpected session index in TMUX value %q", v)
	}
	return Env{Socket: parts[0], PID: pid, SessionIndex: idx}, nil
}

// SessionTarget is the tmux target string ("$N") that names the
// session the variable was exported in.
func (e Env) SessionTarget() string {
	return "$" + strconv.Itoa(e.SessionIndex)
}
