package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// Fake is an in-memory [Backend] used by tests that need to exercise
// the session service without spawning a real tmux process.
//
// Fake is safe for concurrent use; tests typically share one instance
// across goroutines.
type Fake struct {
	mu       sync.Mutex
	sessions map[string]FakeSession
	captures map[string]string
	failNext map[string]error
}

// FakeSession records the arguments passed to NewSession so tests can
// assert on them.
type FakeSession struct {
	Name       string
	WorkingDir string
	Command    string
	Argv       []string
	Env        map[string]string
	SentKeys   []string
}

// NewFake constructs a Fake with empty internal maps.
func NewFake() *Fake {
	return &Fake{
		sessions: make(map[string]FakeSession),
		captures: make(map[string]string),
		failNext: make(map[string]error),
	}
}

// Sessions returns a snapshot of the sessions currently tracked by the
// fake. The returned map is a copy and can be mutated by callers.
func (f *Fake) Sessions() map[string]FakeSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]FakeSession, len(f.sessions))
	for k, v := range f.sessions {
		out[k] = v
	}
	return out
}

// SetCapture seeds the value returned by Capture for the given session.
func (f *Fake) SetCapture(name, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captures[name] = body
}

// FailOnce schedules an error to be returned the next time the named
// operation runs. Operations are addressed by short tags ("new",
// "kill", "list", "capture", "has").
func (f *Fake) FailOnce(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext[op] = err
}

func (f *Fake) consumeFailure(op string) error {
	if err, ok := f.failNext[op]; ok {
		delete(f.failNext, op)
		return err
	}
	return nil
}

// HasSession implements [Backend.HasSession].
func (f *Fake) HasSession(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("has"); err != nil {
		return false, err
	}
	_, ok := f.sessions[name]
	return ok, nil
}

// NewSession implements [Backend.NewSession].
func (f *Fake) NewSession(_ context.Context, opts NewSessionOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("new"); err != nil {
		return err
	}
	if _, exists := f.sessions[opts.Name]; exists {
		return fmt.Errorf("fake tmux: session %q already exists", opts.Name)
	}
	envCopy := make(map[string]string, len(opts.Env))
	for k, v := range opts.Env {
		envCopy[k] = v
	}
	f.sessions[opts.Name] = FakeSession{
		Name:       opts.Name,
		WorkingDir: opts.WorkingDir,
		Command:    opts.Command,
		Argv:       append([]string(nil), opts.Argv...),
		Env:        envCopy,
	}
	return nil
}

// Kill implements [Backend.Kill].
func (f *Fake) Kill(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("kill"); err != nil {
		return err
	}
	delete(f.sessions, name)
	return nil
}

// List implements [Backend.List].
func (f *Fake) List(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("list"); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.sessions))
	for k := range f.sessions {
		out = append(out, k)
	}
	return out, nil
}

// Capture implements [Backend.Capture].
func (f *Fake) Capture(_ context.Context, name string, _ CaptureOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("capture"); err != nil {
		return "", err
	}
	if _, ok := f.sessions[name]; !ok {
		return "", errTmuxNoSession
	}
	return f.captures[name], nil
}

// AttachCommand implements [Backend.AttachCommand]. The returned
// command is a no-op `true` invocation so tests can call cmd.Run()
// without spawning anything meaningful.
func (f *Fake) AttachCommand(name string) *exec.Cmd {
	return exec.Command("true")
}

// SendKeys implements [Backend.SendKeys]. Recorded keystrokes live
// on the FakeSession so tests can assert "delivery happened" or
// "exactly this many messages were sent".
func (f *Fake) SendKeys(_ context.Context, name, keys string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.consumeFailure("send"); err != nil {
		return err
	}
	sess, ok := f.sessions[name]
	if !ok {
		return errTmuxNoSession
	}
	sess.SentKeys = append(sess.SentKeys, keys)
	f.sessions[name] = sess
	return nil
}
