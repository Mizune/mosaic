package pidguard_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Mizune/mosaic/internal/pidguard"
)

func TestAcquire_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.pid")

	release, err := pidguard.Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestAcquire_RejectsLiveProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.pid")

	release, err := pidguard.Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	_, err = pidguard.Acquire(path)
	if err == nil {
		t.Fatal("expected error for duplicate acquire")
	}
}

func TestAcquire_RemovesStalePID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.pid")

	// Write a PID that does not exist (very high number).
	if err := os.WriteFile(path, []byte("9999999"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	release, err := pidguard.Acquire(path)
	if err != nil {
		t.Fatalf("acquire over stale: %v", err)
	}
	defer release()
}

func TestRelease_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.pid")

	release, err := pidguard.Acquire(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("pid file should be removed after release")
	}
}

func TestIsStale_NoFile(t *testing.T) {
	if pidguard.IsStale("/nonexistent/path/mosaic.pid") {
		t.Fatal("should not be stale when file does not exist")
	}
}

func TestRemoveStale_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.pid")

	if err := os.WriteFile(path, []byte("9999999"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if !pidguard.RemoveStale(path) {
		t.Fatal("expected stale removal to succeed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be removed")
	}
}
