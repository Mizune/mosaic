package live

import (
	"context"

	"github.com/Mizune/mosaic/internal/artifact"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/store"
)

// ArtifactDetected is published on the bus after a NEW artifact row has
// been written, so the TUI refreshes at once instead of waiting for its
// 1 Hz tick. Replayed (already known) artifacts publish nothing.
type ArtifactDetected struct {
	SessionID  string // mosaic session id (store.Session.ID)
	ArtifactID string
	Kind       string
	Title      string
}

// recordArtifacts detects rich content (Mermaid blocks, image and HTML
// files written by Write/Edit) in one transcript entry and persists it
// under the parent session. Rows are keyed by content hash, so the
// full-transcript replay that happens on every mosaic start inserts
// nothing new and publishes no events.
func (r *Reconciler) recordArtifacts(ctx context.Context, parent store.Session, ev claudeio.TranscriptEntry) {
	for _, d := range artifact.DetectFromEntry(ev.Entry, parent.WorktreePath) {
		row, created, err := r.store.Artifacts().Upsert(ctx, store.NewArtifact{
			SessionID:   parent.ID,
			Kind:        string(d.Kind),
			Source:      string(artifact.SourceTranscript),
			AgentID:     ev.AgentID,
			Title:       d.Title,
			Content:     d.Content,
			Path:        d.Path,
			ContentHash: d.Hash,
			CreatedAt:   d.At,
		})
		if err != nil {
			r.log.Warn("upsert artifact", "session", parent.ID, "kind", d.Kind, "err", err)
			continue
		}
		if created && r.bus != nil {
			r.bus.Publish(ArtifactDetected{SessionID: parent.ID, ArtifactID: row.ID, Kind: row.Kind, Title: row.Title})
		}
	}
}
