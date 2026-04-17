package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestWrap_NoneReturnsArgvUnchanged documents the fast-path guarantee:
// a session that did not opt into a sandbox pays no transform cost
// and receives the exact slice it passed in.
func TestWrap_NoneReturnsArgvUnchanged(t *testing.T) {
	in := Input{
		SessionID:  "01TEST",
		WorkingDir: "/ignored",
		Env:        map[string]string{"FOO": "bar"},
		Argv:       []string{"claude", "--continue"},
	}
	got := Wrap(Spec{Mode: ModeNone}, in)
	if !reflect.DeepEqual(got, in.Argv) {
		t.Fatalf("Mode=none should return original argv\n want %v\n  got %v", in.Argv, got)
	}
}

// TestWrap_EmptyModeIsNone treats the zero value as "no sandbox".
func TestWrap_EmptyModeIsNone(t *testing.T) {
	in := Input{Argv: []string{"sh"}}
	got := Wrap(Spec{}, in)
	if !reflect.DeepEqual(got, in.Argv) {
		t.Fatalf("empty Mode should behave like ModeNone: got %v", got)
	}
}

// TestWrap_DockerBuildsCanonicalArgv covers the common shape: one
// workspace bind, one RO auth mount, one env, one extra arg, an
// image, and the trailing agent command.
func TestWrap_DockerBuildsCanonicalArgv(t *testing.T) {
	// Override the host-existence check so the test never touches
	// the developer's actual filesystem.
	existing := map[string]bool{
		"/home/alice":         true,
		"/home/alice/.claude": true,
		"/tmp/mosaic-sockets": true,
	}
	prev := hostPathExists
	defer func() { hostPathExists = prev }()
	hostPathExists = func(p string) bool { return existing[p] }

	spec := Spec{
		Mode:  ModeDocker,
		Image: "mosaic/sandbox:latest",
		Mounts: []Mount{
			{Host: "/home/alice/.claude", Container: "/home/mosaic/.claude", ReadOnly: true},
			// Host path missing → should be dropped.
			{Host: "/home/alice/.ssh", Container: "/home/mosaic/.ssh", ReadOnly: true},
		},
		ExtraArgs: []string{"--cpus=2"},
	}
	in := Input{
		SessionID:  "01J123",
		WorkingDir: "/home/alice",
		Env: map[string]string{
			"MOSAIC_SOCKET_PATH": "/tmp/mosaic-sockets/hooks.sock",
			"MOSAIC_WORKTREE_ID": "01J123",
		},
		Argv: []string{"claude", "--dangerously-skip-permissions"},
	}

	got := Wrap(spec, in)

	want := []string{
		"docker", "run", "--rm", "-i", "--init",
		"--name", "mosaic-01J123",
		"-w", "/workspace", "-v", "/home/alice:/workspace",
		"-v", "/home/alice/.claude:/home/mosaic/.claude:ro",
		"-v", "/tmp/mosaic-sockets:/tmp/mosaic-sockets",
		"-e", "MOSAIC_SOCKET_PATH=/tmp/mosaic-sockets/hooks.sock",
		"-e", "MOSAIC_WORKTREE_ID=01J123",
		"--cpus=2",
		"mosaic/sandbox:latest",
		"claude", "--dangerously-skip-permissions",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("docker argv mismatch\n want %v\n  got %v", want, got)
	}
}

// TestWrap_DockerSkipsMissingMounts makes the "silent drop" behaviour
// explicit. When none of the auth mounts exist, only the workspace
// bind should appear.
func TestWrap_DockerSkipsMissingMounts(t *testing.T) {
	tmp := t.TempDir()
	prev := hostPathExists
	defer func() { hostPathExists = prev }()
	hostPathExists = func(p string) bool {
		// Only the working dir exists.
		return filepath.Clean(p) == filepath.Clean(tmp)
	}

	spec := Spec{
		Mode:  ModeDocker,
		Image: "mosaic/sandbox:latest",
		Mounts: []Mount{
			{Host: "/nope/a", Container: "/c/a", ReadOnly: true},
			{Host: "/nope/b", Container: "/c/b", ReadOnly: true},
		},
	}
	in := Input{SessionID: "01X", WorkingDir: tmp, Argv: []string{"bash"}}

	got := Wrap(spec, in)

	// Assert no `-v <host>:<container>:ro` for the missing mounts.
	for _, arg := range got {
		if strings.Contains(arg, "/nope/") {
			t.Fatalf("missing-host mount leaked into argv: %q\n full: %v", arg, got)
		}
	}
	// The workspace bind should still be present.
	foundWorkspace := false
	for i, arg := range got {
		if arg == "-v" && i+1 < len(got) && got[i+1] == tmp+":/workspace" {
			foundWorkspace = true
			break
		}
	}
	if !foundWorkspace {
		t.Fatalf("workspace bind missing from argv: %v", got)
	}
}

// TestWrap_DockerIsDeterministic guards the sort contract: iterating
// over the env map in different orders must not change the resulting
// argv. Flaky argv would poison test diffs and container names.
func TestWrap_DockerIsDeterministic(t *testing.T) {
	prev := hostPathExists
	defer func() { hostPathExists = prev }()
	hostPathExists = func(string) bool { return false }

	spec := Spec{Mode: ModeDocker, Image: "x:y"}
	in := Input{
		SessionID:  "01Z",
		WorkingDir: "/w",
		Env: map[string]string{
			"Z": "1", "A": "2", "M": "3",
		},
		Argv: []string{"cmd"},
	}
	a := Wrap(spec, in)
	b := Wrap(spec, in)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Wrap should be deterministic:\n a=%v\n b=%v", a, b)
	}
}

// TestContainerNamePattern pins the convention so external cleanup
// scripts (e.g. `docker ps --filter name=mosaic-`) keep working.
func TestContainerNamePattern(t *testing.T) {
	if got := ContainerName("01ABC"); got != "mosaic-01ABC" {
		t.Fatalf("ContainerName(01ABC) = %q, want mosaic-01ABC", got)
	}
}

// TestParseModeFallsBackToNone ensures a typo in config never
// silently enables sandboxing.
func TestParseModeFallsBackToNone(t *testing.T) {
	cases := map[string]Mode{
		"":       ModeNone,
		"none":   ModeNone,
		"docker": ModeDocker,
		"typo":   ModeNone,
		"DOCKER": ModeNone, // case-sensitive on purpose
	}
	for input, want := range cases {
		if got := ParseMode(input); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", input, got, want)
		}
	}
}

// ensure os.Stat default is wired correctly (sanity check that the
// production DockerAvailable path doesn't panic on a missing binary).
func TestHostPathExistsOnTempFile(t *testing.T) {
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "probe")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !hostPathExists(f.Name()) {
		t.Fatalf("hostPathExists returned false for an existing file: %s", f.Name())
	}
	if hostPathExists(filepath.Join(tmp, "definitely-not-there")) {
		t.Fatalf("hostPathExists returned true for a missing file")
	}
}
