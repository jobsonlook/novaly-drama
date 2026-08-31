package services

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
)

var gridCandidateSuffixRe = regexp.MustCompile(`·\s*候选\s*\d+\s*$`)

// SceneGridBaseName returns the bare scene name from a grid or grid-cell name:
// strips the " · 9宫格…" provenance and trailing 候选N / angle segments.
func SceneGridBaseName(name string) string {
	n := strings.TrimSpace(name)
	for _, sep := range []string{" · 9宫格", " · 9帧图"} {
		if i := strings.Index(n, sep); i > 0 {
			return strings.TrimSpace(n[:i])
		}
	}
	n = gridCandidateSuffixRe.ReplaceAllString(n, "")
	if i := strings.LastIndex(n, "·"); i > 0 {
		maybeAngle := strings.TrimSpace(n[i+1:])
		for _, a := range SceneGridAngles {
			if maybeAngle == a {
				n = strings.TrimSpace(n[:i])
				break
			}
		}
	}
	return n
}

var scenePlateStemSuffixes = []string{"站位图", "反打骨架", "反打"}

// ScenePlateStem strips 9-grid / reverse / positioning suffixes so a standing
// plate like「包厢站位图」can match the empty-room grid「私人会所包厢 · 9宫格」.
func ScenePlateStem(name string) string {
	n := SceneGridBaseName(name)
	for {
		next := n
		for _, suf := range scenePlateStemSuffixes {
			next = strings.TrimSpace(strings.TrimSuffix(next, suf))
			next = strings.Trim(next, " ·-")
		}
		if next == n {
			return n
		}
		n = next
	}
}

// SceneGridMatchesPlate reports whether a 9-grid belongs to the same room as a
// scene/standing plate. Exact base match, or one name contains the other's stem
// (at least 2 runes) so「包厢站位图」pairs with「私人会所包厢 · 9宫格」.
func SceneGridMatchesPlate(gridName, plateName string) bool {
	gridBase := strings.TrimSpace(SceneGridBaseName(gridName))
	plate := strings.TrimSpace(SceneGridBaseName(plateName))
	if gridBase == "" || plate == "" {
		return false
	}
	if gridBase == plate {
		return true
	}
	gridStem := ScenePlateStem(gridBase)
	plateStem := ScenePlateStem(plate)
	if gridStem == plateStem && gridStem != "" {
		return true
	}
	contains := func(haystack, needle string) bool {
		return len([]rune(needle)) >= 2 && strings.Contains(haystack, needle)
	}
	return contains(gridBase, plateStem) || contains(plate, gridStem) || contains(gridStem, plateStem) || contains(plateStem, gridStem)
}

// SceneGridCellName builds the canonical short cell name: 场景名·机位名.
// Do not append 候选N — cells are identified by grid_id + grid_cell, and a
// trailing 候选N makes library candidate backfill treat them as disposable drafts.
// Returns "" when the cell index has no fixed angle.
func SceneGridCellName(gridName string, cell int) string {
	base := SceneGridBaseName(gridName)
	angle := SceneAngleLabel(cell)
	if base == "" || angle == "" {
		return ""
	}
	return base + "·" + angle
}

// SplitGridImage crops a 3×3 grid image into 9 cell images (row-major, cell 1..9).
// Each cell is re-encoded as JPEG so it matches the .jpg persistence convention.
func SplitGridImage(data []byte) ([][]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解析9宫格图片失败：%w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 30 || h < 30 {
		return nil, fmt.Errorf("图片尺寸 %dx%d 过小，无法切分为9宫格", w, h)
	}
	cells := make([][]byte, 0, 9)
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			x0 := b.Min.X + col*w/3
			x1 := b.Min.X + (col+1)*w/3
			y0 := b.Min.Y + row*h/3
			y1 := b.Min.Y + (row+1)*h/3
			cell := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
			draw.Draw(cell, cell.Bounds(), src, image.Pt(x0, y0), draw.Src)
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, cell, &jpeg.Options{Quality: 92}); err != nil {
				return nil, fmt.Errorf("编码第%d格失败：%w", row*3+col+1, err)
			}
			cells = append(cells, buf.Bytes())
		}
	}
	return cells, nil
}
