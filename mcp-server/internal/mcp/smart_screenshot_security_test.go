package mcp

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"testing"

	"browsernerd-mcp-server/internal/security"
)

func TestDrawBoundingBoxOverlaysPreservesRequestedFormat(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			source.Set(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
		}
	}
	boxes := []BoundingBox{{Index: 1, X: 2, Y: 2, Width: 20, Height: 10, Type: "button"}}

	var pngInput bytes.Buffer
	if err := png.Encode(&pngInput, source); err != nil {
		t.Fatal(err)
	}
	pngOutput, err := drawBoundingBoxOverlays(pngInput.Bytes(), boxes, "png", 90)
	if err != nil {
		t.Fatalf("PNG overlay failed: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(pngOutput)); err != nil {
		t.Fatalf("PNG output is invalid: %v", err)
	}

	var jpegInput bytes.Buffer
	if err := jpeg.Encode(&jpegInput, source, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	jpegOutput, err := drawBoundingBoxOverlays(jpegInput.Bytes(), boxes, "jpeg", 85)
	if err != nil {
		t.Fatalf("JPEG overlay failed: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(jpegOutput)); err != nil {
		t.Fatalf("JPEG output is invalid: %v", err)
	}
	if bytes.HasPrefix(jpegOutput, []byte("\x89PNG")) {
		t.Fatal("JPEG output was silently re-encoded as PNG")
	}
}

func TestResolveFlightExportPathRejectsUnconfinedWrite(t *testing.T) {
	base := t.TempDir()
	traceDir := filepath.Join(base, "traces")
	policy, err := security.NewPathPolicy(base, []string{traceDir})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside", "evidence.jsonl")
	if _, err := resolveFlightExportPath(policy, outside, traceDir, "session", 1); err == nil {
		t.Fatal("expected export outside writable roots to be rejected")
	}
}
