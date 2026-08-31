package services

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestEquirectangularToPerspectiveSafeSeamFront(t *testing.T) {
	// 400x200 panorama with a bright red vertical band at x≈25% (safe-seam front).
	const w, h = 400, 200
	pano := image.NewNRGBA(image.Rect(0, 0, w, h))
	frontX0, frontX1 := w/4-8, w/4+8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= frontX0 && x < frontX1 {
				pano.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				pano.SetNRGBA(x, y, color.NRGBA{B: 40, A: 255})
			}
		}
	}

	view := EquirectangularToPerspective(pano, SafeSeamFrontYawDeg, 0, 90, 64, 36)
	nrgba, ok := view.(*image.NRGBA)
	if !ok {
		t.Fatalf("expected *image.NRGBA, got %T", view)
	}
	cx, cy := 32, 18
	c := nrgba.NRGBAAt(cx, cy)
	if c.R < 200 {
		t.Fatalf("front crop center should be red (safe-seam), got %+v", c)
	}
}

func TestSplitPanoramaViews(t *testing.T) {
	const w, h = 400, 200
	pano := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pano.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, pano); err != nil {
		t.Fatal(err)
	}
	views, err := SplitPanoramaViews(buf.Bytes(), 128, 72)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 6 {
		t.Fatalf("want 6 views, got %d", len(views))
	}
	for _, v := range views {
		if v.Key == "" || v.Label == "" || len(v.JPEG) < 100 {
			t.Fatalf("bad view: %+v len=%d", v, len(v.JPEG))
		}
	}
}

func TestSplitPanoramaViewsRejectsNonPano(t *testing.T) {
	sq := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	var buf bytes.Buffer
	if err := png.Encode(&buf, sq); err != nil {
		t.Fatal(err)
	}
	if _, err := SplitPanoramaViews(buf.Bytes(), 64, 64); err == nil {
		t.Fatal("expected error for non-2:1 image")
	}
}
