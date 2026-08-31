package services

import (
	"regexp"
	"strings"
)

var (
	sceneAngleBeatRE   = regexp.MustCompile(`【[^】]*秒】[^【]*`)
	sceneAngleMarkerRE = regexp.MustCompile(`机位：\s*([^\n，。；】]+)`)
)

// InferWantedSceneAngles picks up to 3 scene-9-grid angle labels from a shot
// script (explicit 机位：… plus 景别 / 运镜 cues, scanned per timing beat).
func InferWantedSceneAngles(script string) []string {
	script = strings.TrimSpace(script)
	if script == "" {
		return []string{"正面近景"}
	}
	wanted := make([]string, 0, 3)
	push := func(angles ...string) {
		for _, a := range angles {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			ok := false
			for _, known := range SceneGridAngles {
				if a == known {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
			for _, have := range wanted {
				if have == a {
					return
				}
			}
			wanted = append(wanted, a)
		}
	}

	for _, m := range sceneAngleMarkerRE.FindAllStringSubmatch(script, -1) {
		if len(m) < 2 {
			continue
		}
		a := strings.TrimSpace(m[1])
		if i := strings.IndexAny(a, "（("); i > 0 {
			a = strings.TrimSpace(a[:i])
		}
		push(a)
	}

	beats := sceneAngleBeatRE.FindAllString(script, -1)
	if len(beats) == 0 {
		beats = []string{script}
	}
	for _, beat := range beats {
		if len(wanted) >= 3 {
			break
		}
		push(anglesFromShotSizeCue(beat)...)
	}
	if len(wanted) == 0 {
		push("正面近景")
	}
	if len(wanted) > 3 {
		return wanted[:3]
	}
	return wanted
}

func anglesFromShotSizeCue(text string) []string {
	switch {
	case strings.Contains(text, "俯视") || strings.Contains(text, "鸟瞰") || strings.Contains(text, "高位"):
		return []string{"俯视近景", "斜向高位总览"}
	case strings.Contains(text, "背面") || strings.Contains(text, "背影"):
		return []string{"背面近景", "背面全景"}
	case strings.Contains(text, "侧面"):
		return []string{"侧面近景", "侧面全景"}
	case strings.Contains(text, "特写") || strings.Contains(text, "近景") || strings.Contains(text, "中近景"):
		return []string{"正面近景"}
	case strings.Contains(text, "全景") || strings.Contains(text, "远景") || strings.Contains(text, "空镜"):
		return []string{"正面全景", "侧面全景"}
	case strings.Contains(text, "中景"):
		return []string{"正面近景", "侧面全景"}
	default:
		return nil
	}
}

// SceneGridCellPick is one preferred cell for a shot.
type SceneGridCellPick struct {
	ID    uint
	Angle string
	Label string
}

// SceneGridCellCandidate is a split 9-grid cell available for picking.
type SceneGridCellCandidate struct {
	ID       uint
	Name     string
	GridCell int
	GridID   uint
}

// PickSceneGridCellsForScript chooses up to max cells whose angles match the script.
// cells should already be filtered to one scene family / one preferred grid.
func PickSceneGridCellsForScript(cells []SceneGridCellCandidate, script string, max int) []SceneGridCellPick {
	if max <= 0 {
		max = 3
	}
	if len(cells) == 0 {
		return nil
	}
	byAngle := map[string]SceneGridCellCandidate{}
	for _, c := range cells {
		angle := SceneAngleLabel(c.GridCell)
		if angle == "" {
			angle = strings.TrimSpace(c.Name)
			if i := strings.LastIndex(angle, "·"); i >= 0 {
				angle = strings.TrimSpace(angle[i+1:])
			}
		}
		if angle == "" {
			continue
		}
		if _, ok := byAngle[angle]; !ok {
			byAngle[angle] = c
		}
	}
	wanted := InferWantedSceneAngles(script)
	out := make([]SceneGridCellPick, 0, max)
	seen := map[uint]bool{}
	for _, angle := range wanted {
		c, ok := byAngle[angle]
		if !ok || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		label := strings.TrimSpace(c.Name)
		if label == "" {
			base := SceneGridBaseName(c.Name)
			if base != "" {
				label = base + "·" + angle
			} else {
				label = angle
			}
		}
		out = append(out, SceneGridCellPick{ID: c.ID, Angle: angle, Label: label})
		if len(out) >= max {
			return out
		}
	}
	if len(out) == 0 {
		fallback := byAngle["正面近景"]
		if fallback.ID == 0 {
			fallback = cells[0]
		}
		angle := SceneAngleLabel(fallback.GridCell)
		if angle == "" {
			angle = "正面近景"
		}
		label := strings.TrimSpace(fallback.Name)
		if label == "" {
			label = angle
		}
		out = append(out, SceneGridCellPick{ID: fallback.ID, Angle: angle, Label: label})
	}
	return out
}

// SkeletonSceneCandidate is a scene plate or 9-grid cell that can ground stick-figure blocking.
type SkeletonSceneCandidate struct {
	ID       uint
	Type     string
	GenType  string
	Name     string
	GridCell int
}

func (c SkeletonSceneCandidate) isGridCell() bool {
	return c.GenType == "scene_grid_cell"
}

func (c SkeletonSceneCandidate) isScenePlate() bool {
	if c.GenType == "scene_grid" || c.GenType == "scene_grid_cell" {
		return false
	}
	return c.Type == "scene" || c.GenType == "scene"
}

// PickSkeletonSceneRef chooses 1 spatial plate for the stick-figure pass.
// Prefer a 9-grid cell matching the script camera angle; never the full 3×3 collage.
func PickSkeletonSceneRef(cands []SkeletonSceneCandidate, script string) *SkeletonSceneCandidate {
	if len(cands) == 0 {
		return nil
	}
	cells := make([]SceneGridCellCandidate, 0, len(cands))
	cellByID := map[uint]SkeletonSceneCandidate{}
	var plates []SkeletonSceneCandidate
	for _, c := range cands {
		if c.GenType == "scene_grid" {
			continue
		}
		if c.isGridCell() {
			cells = append(cells, SceneGridCellCandidate{ID: c.ID, Name: c.Name, GridCell: c.GridCell})
			if _, ok := cellByID[c.ID]; !ok {
				cellByID[c.ID] = c
			}
			continue
		}
		if c.isScenePlate() {
			plates = append(plates, c)
		}
	}
	if picks := PickSceneGridCellsForScript(cells, script, 1); len(picks) > 0 {
		if c, ok := cellByID[picks[0].ID]; ok {
			cp := c
			return &cp
		}
	}
	if len(plates) > 0 {
		p := plates[0]
		return &p
	}
	return nil
}
