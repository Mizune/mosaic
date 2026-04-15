package claudeio

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sync"

	"github.com/Mizune/mosaic/pkg/transcript"
)

// Tail keeps a per-file byte offset and yields newly appended JSONL
// entries on each Read call. Tails are tied to one file path; the
// owning watcher creates one Tail per transcript it cares about.
type Tail struct {
	mu     sync.Mutex
	path   string
	offset int64
}

// NewTail constructs a Tail starting at offset 0.
func NewTail(path string) *Tail {
	return &Tail{path: path}
}

// NewTailAtOffset constructs a Tail that resumes from the given byte
// offset. Used after restarts to skip entries already persisted to
// SQLite.
func NewTailAtOffset(path string, offset int64) *Tail {
	return &Tail{path: path, offset: offset}
}

// Path returns the file the tail is bound to.
func (t *Tail) Path() string { return t.path }

// Offset returns the next byte offset that Read would consume.
func (t *Tail) Offset() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.offset
}

// Read consumes everything appended to the file since the last Read
// and returns the parsed entries plus the new offset.
func (t *Tail) Read() ([]transcript.Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	f, err := os.Open(t.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size < t.offset {
		// File was truncated / rotated. Restart from the top.
		t.offset = 0
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}

	dec := transcript.NewDecoder(f)
	var entries []transcript.Entry
	for {
		e, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Permanent scanner error (e.g. bufio.ErrTooLong for
			// lines exceeding the 16 MiB buffer). The scanner will
			// never yield another line, so we must break to avoid an
			// infinite spin loop. The offset advances to the current
			// file size, so the next Read() picks up new content.
			// Lines between the error and the end of this batch are
			// lost; log so monitoring can detect data loss.
			slog.Warn("tail: scanner error, skipping to end of file",
				"path", t.path, "offset", t.offset, "err", err)
			break
		}
		entries = append(entries, e)
	}
	t.offset = size
	return entries, nil
}
