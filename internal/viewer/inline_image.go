package viewer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	// Decoders for the image kinds converted to PNG without a browser.
	_ "image/gif"
	_ "image/jpeg"
)

// InlineImage is a PNG ready to be transmitted to the terminal.
type InlineImage struct {
	PNG    []byte
	Width  int
	Height int
}

// Raster canvas for pages. The terminal scales the result into the
// preview box, so this only bounds detail and file size.
const (
	rasterWidth  = 1400
	rasterHeight = 900
)

// PrepareInline produces the PNG for t. PNG files are used as they are;
// JPEG and GIF are re-encoded; everything else (Mermaid pages, HTML,
// SVG, WebP, URLs) is rasterized with r. Results are cached as
// <CacheRoot>/<session>/<hash>.png next to the generated pages, so the
// second preview of an artifact is instant and Kill removes them.
func PrepareInline(ctx context.Context, t Target, pages *PageWriter, opts Options, r *Rasterizer) (InlineImage, error) {
	if err := checkPathComponent("session id", t.SessionID); err != nil {
		return InlineImage{}, err
	}
	hash := t.Hash
	if hash == "" {
		hash = contentHash(t.Content + "\x00" + t.Path)
	}
	if err := checkPathComponent("hash", hash); err != nil {
		return InlineImage{}, err
	}
	dir := filepath.Join(opts.CacheRoot, t.SessionID)
	cached := filepath.Join(dir, hash+".png")
	if data, err := os.ReadFile(cached); err == nil {
		if img, derr := decodeInline(data); derr == nil {
			return img, nil
		}
	}

	if t.Kind == KindImage {
		switch strings.ToLower(filepath.Ext(t.Path)) {
		case ".png":
			return readPNG(t.Path)
		case ".jpg", ".jpeg", ".gif":
			return convertToPNG(t.Path, dir, cached)
		}
	}

	url, err := rasterSource(t, pages, opts)
	if err != nil {
		return InlineImage{}, err
	}
	if !r.Available() {
		return InlineImage{}, ErrNoRasterizer
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return InlineImage{}, fmt.Errorf("viewer: create cache dir: %w", err)
	}
	if err := r.Screenshot(ctx, url, cached, rasterWidth, rasterHeight); err != nil {
		return InlineImage{}, err
	}
	data, err := os.ReadFile(cached)
	if err != nil {
		return InlineImage{}, fmt.Errorf("viewer: read rasterized image: %w", err)
	}
	return decodeInline(data)
}

// rasterSource is the URL the rasterizer loads for t.
func rasterSource(t Target, pages *PageWriter, opts Options) (string, error) {
	switch t.Kind {
	case KindMermaid:
		if pages == nil {
			pages = &PageWriter{Root: opts.CacheRoot}
		}
		path, err := pages.WriteMermaid(t, opts.MermaidJSURL)
		if err != nil {
			return "", err
		}
		return FileURL(path), nil
	case KindImage, KindHTML:
		return fileTargetURL(t.Path)
	case KindURL:
		raw := t.Content
		if raw == "" {
			raw = t.Path
		}
		return validateURL(raw)
	default:
		return "", fmt.Errorf("viewer: unsupported artifact kind %q", t.Kind)
	}
}

func readPNG(path string) (InlineImage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return InlineImage{}, ErrArtifactMissing
	}
	if err != nil {
		return InlineImage{}, fmt.Errorf("viewer: read image: %w", err)
	}
	return decodeInline(data)
}

// convertToPNG decodes a JPEG or GIF and caches it as PNG. A failed
// cache write is not an error: the image is still returned.
func convertToPNG(src, dir, cached string) (InlineImage, error) {
	f, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return InlineImage{}, ErrArtifactMissing
	}
	if err != nil {
		return InlineImage{}, fmt.Errorf("viewer: open image: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return InlineImage{}, fmt.Errorf("viewer: decode image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return InlineImage{}, fmt.Errorf("viewer: encode png: %w", err)
	}
	if os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(cached, buf.Bytes(), 0o600)
	}
	b := img.Bounds()
	return InlineImage{PNG: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

func decodeInline(data []byte) (InlineImage, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return InlineImage{}, fmt.Errorf("viewer: not a png: %w", err)
	}
	return InlineImage{PNG: data, Width: cfg.Width, Height: cfg.Height}, nil
}
