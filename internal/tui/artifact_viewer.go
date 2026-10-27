package tui

import (
	"context"
	"time"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/viewer"
)

// viewerAdapter satisfies artifactViewer with the real viewer package:
// it converts a store row into a viewer.Target, resolves the URL
// (writing a Mermaid page when needed) and hands it to the launcher
// picked for this environment (cmux browser pane or OS browser).
type viewerAdapter struct {
	launcher viewer.Launcher
	pages    *viewer.PageWriter
	opts     viewer.Options
	raster   *viewer.Rasterizer
	inline   viewer.InlineSupport
}

func newViewerAdapter(cfg config.ViewerSection) *viewerAdapter {
	opts := viewer.OptionsFrom(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return &viewerAdapter{
		launcher: viewer.New(opts),
		pages:    &viewer.PageWriter{Root: opts.CacheRoot},
		opts:     opts,
		raster:   viewer.NewRasterizer(opts.ChromePath, opts.CacheRoot),
		inline:   viewer.DetectInline(ctx, opts.Inline, viewer.DefaultEnvironment()),
	}
}

func (a *viewerAdapter) Name() string { return a.launcher.Name() }

// InlineSupport implements inlinePreviewer.
func (a *viewerAdapter) InlineSupport() (bool, string) {
	return a.inline.Supported, a.inline.Reason
}

// RenderInline implements inlinePreviewer: rasterize (or read) the
// artifact as PNG and encode it for the terminal.
func (a *viewerAdapter) RenderInline(ctx context.Context, row store.Artifact, id, cols, rows int) (viewer.KittyPlacement, error) {
	img, err := viewer.PrepareInline(ctx, targetFor(row), a.pages, a.opts, a.raster)
	if err != nil {
		return viewer.KittyPlacement{}, err
	}
	return viewer.EncodeKitty(img, id, cols, rows, a.inline.InTmux)
}

func (a *viewerAdapter) Open(ctx context.Context, row store.Artifact) error {
	url, err := viewer.TargetURL(targetFor(row), a.pages, a.opts)
	if err != nil {
		return err
	}
	return a.launcher.Open(ctx, url)
}

// targetFor maps the persisted row onto the viewer's input. URL rows
// keep the address in Path; the viewer falls back to it when Content
// is empty.
func targetFor(row store.Artifact) viewer.Target {
	return viewer.Target{
		SessionID: row.SessionID,
		Kind:      viewer.Kind(row.Kind),
		Title:     row.Title,
		Content:   row.Content.String,
		Path:      row.Path,
		Hash:      row.ContentHash,
	}
}
