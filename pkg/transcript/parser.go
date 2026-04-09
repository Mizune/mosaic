package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

// ParseLine decodes a single JSONL line. Empty lines yield (Entry{},
// io.EOF) so callers can use it as a "is this a real entry?" probe.
func ParseLine(line []byte) (Entry, error) {
	if len(line) == 0 {
		return Entry{}, io.EOF
	}
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return Entry{}, err
	}
	e.Raw = append(e.Raw[:0], line...)
	return e, nil
}

// Decoder is a buffered reader that yields Entry values from a stream
// of newline-delimited JSON. It is intended for one-shot parsing in
// tests and for the byte-offset tail loop in the watcher.
type Decoder struct {
	scanner *bufio.Scanner
}

// NewDecoder constructs a Decoder around r. The scanner buffer is
// raised to 4 MiB so it can handle pathologically long assistant
// responses without truncation.
func NewDecoder(r io.Reader) *Decoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &Decoder{scanner: sc}
}

// Next reads the next entry. It returns io.EOF when the stream is
// exhausted, or any underlying error from the bufio.Scanner /
// json.Unmarshal.
func (d *Decoder) Next() (Entry, error) {
	for d.scanner.Scan() {
		line := d.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		entry, err := ParseLine(line)
		if errors.Is(err, io.EOF) {
			continue
		}
		return entry, err
	}
	if err := d.scanner.Err(); err != nil {
		return Entry{}, err
	}
	return Entry{}, io.EOF
}
