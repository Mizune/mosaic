// Package artifact detects rich content an agent produced while it
// worked — Mermaid diagrams written into the transcript, image or HTML
// files it saved to disk — and gives every item a stable identity so the
// same content is recorded once no matter how often a transcript is
// replayed. It depends only on the standard library and pkg/transcript;
// persistence lives in internal/store and display in internal/viewer.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind classifies an artifact and therefore how the viewer opens it.
type Kind string

const (
	// KindMermaid is a Mermaid diagram whose source is stored inline.
	KindMermaid Kind = "mermaid"
	// KindImage is a raster or SVG image file on disk.
	KindImage Kind = "image"
	// KindHTML is an HTML document on disk.
	KindHTML Kind = "html"
	// KindURL is an http(s) or file URL the user typed by hand.
	KindURL Kind = "url"
)

// Source records how an artifact entered mosaic.
type Source string

const (
	// SourceTranscript marks artifacts detected in an agent transcript.
	SourceTranscript Source = "transcript"
	// SourceManual marks artifacts the user registered from the picker.
	SourceManual Source = "manual"
)

// Detected is one artifact found in a transcript entry, before it is
// persisted. Content is set for Mermaid diagrams; Path for files.
type Detected struct {
	Kind    Kind
	Title   string
	Content string
	Path    string
	Hash    string
	At      time.Time
}

// hashLen is the number of hex characters kept from the SHA-256 digest.
// 64 bits is plenty to tell one diagram from another within a session.
const hashLen = 16

// ContentHash returns a short, stable identifier for s. Mermaid
// artifacts hash their source; file artifacts hash their absolute path
// so repeated edits of one file collapse into a single row.
func ContentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:hashLen]
}

// kindByExt maps lower-case file extensions to the artifact kind.
var kindByExt = map[string]Kind{
	".png":  KindImage,
	".jpg":  KindImage,
	".jpeg": KindImage,
	".gif":  KindImage,
	".webp": KindImage,
	".svg":  KindImage,
	".html": KindHTML,
	".htm":  KindHTML,
}

// KindForPath reports the artifact kind implied by p's extension, or
// false when the file type is not something the viewer can show.
func KindForPath(p string) (Kind, bool) {
	k, ok := kindByExt[strings.ToLower(filepath.Ext(p))]
	return k, ok
}

// maxTitleRunes bounds the picker label so a long first line cannot
// push the timestamp column off screen.
const maxTitleRunes = 60

// TitleFor derives a human label from a Mermaid source: its first
// non-empty line (usually the diagram type, e.g. "graph TD"), trimmed
// and cut to maxTitleRunes.
func TitleFor(source string) string {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) <= maxTitleRunes {
			return line
		}
		runes := []rune(line)
		return string(runes[:maxTitleRunes-1]) + "…"
	}
	return "mermaid"
}
