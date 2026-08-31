package services

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

// PanoViewSpec describes one rectilinear crop from an equirectangular panorama.
type PanoViewSpec struct {
	Key      string
	Label    string
	YawDeg   float64
	PitchDeg float64
	FovDeg   float64
}

// PanoViewResult is one JPEG crop produced by SplitPanoramaViews.
type PanoViewResult struct {
	Key   string
	Label string
	JPEG  []byte
}

// SafeSeamFrontYawDeg is the yaw offset for panoramas generated with Novaly's
// Dramaclaw-style safe-seam layout (front center at ~25% width, not 50%).
const SafeSeamFrontYawDeg = -90.0

// DefaultPanoViewSpecs returns front/right/back/left + mild up/down hints.
// Pass SafeSeamFrontYawDeg so "front" samples the safe-seam front center.
func DefaultPanoViewSpecs(frontYawOffsetDeg float64) []PanoViewSpec {
	return []PanoViewSpec{
		{Key: "front", Label: "正面全景", YawDeg: frontYawOffsetDeg + 0, PitchDeg: 0, FovDeg: 90},
		{Key: "right", Label: "右侧全景", YawDeg: frontYawOffsetDeg + 90, PitchDeg: 0, FovDeg: 90},
		{Key: "back", Label: "背面全景", YawDeg: frontYawOffsetDeg + 180, PitchDeg: 0, FovDeg: 90},
		{Key: "left", Label: "左侧全景", YawDeg: frontYawOffsetDeg - 90, PitchDeg: 0, FovDeg: 90},
		{Key: "up", Label: "仰视提示", YawDeg: frontYawOffsetDeg + 0, PitchDeg: -55, FovDeg: 100},
		{Key: "down", Label: "俯视提示", YawDeg: frontYawOffsetDeg + 0, PitchDeg: 55, FovDeg: 100},
	}
}

// EquirectangularToPerspective projects a 2:1 panorama into a rectilinear view.
func EquirectangularToPerspective(pano image.Image, yawDeg, pitchDeg, fovDeg float64, width, height int) image.Image {
	if width < 64 {
		width = 64
	}
	if height < 64 {
		height = 64
	}
	if fovDeg <= 1 {
		fovDeg = 90
	}
	src := imageToNRGBA(pano)
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	if srcW < 2 || srcH < 2 {
		return image.NewNRGBA(image.Rect(0, 0, width, height))
	}

	aspect := float64(width) / float64(height)
	fovY := fovDeg * math.Pi / 180
	fovX := 2 * math.Atan(math.Tan(fovY/2)*aspect)

	pitch := pitchDeg * math.Pi / 180
	yaw := yawDeg * math.Pi / 180
	cp, sp := math.Cos(pitch), math.Sin(pitch)
	cy, sy := math.Cos(yaw), math.Sin(yaw)

	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for py := 0; py < height; py++ {
		for px := 0; px < width; px++ {
			xx := ((float64(px)+0.5)/float64(width))*2 - 1
			yy := 1 - ((float64(py)+0.5)/float64(height))*2
			x := xx * math.Tan(fovX/2)
			y := yy * math.Tan(fovY/2)
			z := 1.0
			norm := math.Sqrt(x*x + y*y + z*z)
			x, y, z = x/norm, y/norm, z/norm

			yPitch := y*cp - z*sp
			zPitch := y*sp + z*cp
			xWorld := x*cy + zPitch*sy
			yWorld := yPitch
			zWorld := -x*sy + zPitch*cy

			lon := math.Atan2(xWorld, zWorld)
			lat := math.Asin(clampFloat(yWorld, -1, 1))
			u := (lon/(2*math.Pi) + 0.5) * float64(srcW)
			v := (0.5 - lat/math.Pi) * float64(srcH)
			out.SetNRGBA(px, py, sampleBilinear(src, u, v))
		}
	}
	return out
}

// SplitPanoramaViews decodes an equirectangular PNG/JPEG and returns JPEG bytes
// for each default camera view (safe-seam front yaw applied).
func SplitPanoramaViews(data []byte, outW, outH int) ([]PanoViewResult, error) {
	if outW <= 0 {
		outW = 1280
	}
	if outH <= 0 {
		outH = 720
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码全景图失败：%w", err)
	}
	b := img.Bounds()
	if b.Dx() < b.Dy()*18/10 {
		return nil, fmt.Errorf("期望约 2:1 全景图，实际为 %dx%d", b.Dx(), b.Dy())
	}
	specs := DefaultPanoViewSpecs(SafeSeamFrontYawDeg)
	out := make([]PanoViewResult, 0, len(specs))
	for _, spec := range specs {
		view := EquirectangularToPerspective(img, spec.YawDeg, spec.PitchDeg, spec.FovDeg, outW, outH)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, view, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("编码 %s 失败：%w", spec.Label, err)
		}
		out = append(out, PanoViewResult{Key: spec.Key, Label: spec.Label, JPEG: buf.Bytes()})
	}
	return out, nil
}

func imageToNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x-b.Min.X, y-b.Min.Y, src.At(x, y))
		}
	}
	return dst
}

func sampleBilinear(src *image.NRGBA, u, v float64) color.NRGBA {
	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return color.NRGBA{}
	}
	for u < 0 {
		u += float64(w)
	}
	u = math.Mod(u, float64(w))
	if v < 0 {
		v = 0
	}
	if v > float64(h-1) {
		v = float64(h - 1)
	}
	u0 := int(math.Floor(u))
	v0 := int(math.Floor(v))
	u1 := (u0 + 1) % w
	v1 := v0 + 1
	if v1 >= h {
		v1 = h - 1
	}
	du := u - float64(u0)
	dv := v - float64(v0)
	c00 := src.NRGBAAt(u0, v0)
	c10 := src.NRGBAAt(u1, v0)
	c01 := src.NRGBAAt(u0, v1)
	c11 := src.NRGBAAt(u1, v1)
	lerp := func(a, b uint8, t float64) uint8 {
		return uint8(math.Round((1-t)*float64(a) + t*float64(b)))
	}
	topR := lerp(c00.R, c10.R, du)
	topG := lerp(c00.G, c10.G, du)
	topB := lerp(c00.B, c10.B, du)
	topA := lerp(c00.A, c10.A, du)
	botR := lerp(c01.R, c11.R, du)
	botG := lerp(c01.G, c11.G, du)
	botB := lerp(c01.B, c11.B, du)
	botA := lerp(c01.A, c11.A, du)
	return color.NRGBA{
		R: lerp(topR, botR, dv),
		G: lerp(topG, botG, dv),
		B: lerp(topB, botB, dv),
		A: lerp(topA, botA, dv),
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
