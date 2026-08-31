package services

import (
	"strings"

	"novaly/backend/models"
)

func normalizeRefQuery(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.NewReplacer(
		"·", " ",
		"•", " ",
		"／", " ",
		"/", " ",
		"－", " ",
		"-", " ",
		"——", " ",
		"（", " ",
		"）", " ",
		"(", " ",
		")", " ",
		"【", " ",
		"】", " ",
		"[", " ",
		"]", " ",
		"，", " ",
		",", " ",
	).Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func stripCandidateSuffix(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, " · 候选"); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	if i := strings.Index(name, "·候选"); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}

// ResourceQueryMatches reports whether a library/asset row is the target of a
// storyboard name. Derivative rows named "赤膊战损" also match "韩铮 · 赤膊战损"
// and "韩铮（赤膊战损）", but not the parent name alone.
func ResourceQueryMatches(resourceName, parentName, query string) bool {
	q := normalizeRefQuery(query)
	n := normalizeRefQuery(stripCandidateSuffix(resourceName))
	if q == "" || n == "" {
		return false
	}
	if q == n {
		return true
	}
	p := normalizeRefQuery(stripCandidateSuffix(parentName))
	if p == "" {
		return false
	}
	display := strings.TrimSpace(p + " " + n)
	return q == display
}

// ResourceIdentityLabel is the name Seedance/豆包 should bind 图N to.
// Derivatives keep the parent identity: 韩铮（赤膊战损）.
func ResourceIdentityLabel(r models.Resource) string {
	name := stripCandidateSuffix(r.Name)
	parent := strings.TrimSpace(r.ParentName)
	if r.ParentID != nil && *r.ParentID > 0 && parent != "" {
		if strings.Contains(name, parent) {
			return name
		}
		return parent + "（" + name + "）"
	}
	return name
}

// VideoRefIdentityLabel prefers a custom 图N alias only when it already carries
// the parent identity. Bare state names like "赤膊战损" are replaced.
func VideoRefIdentityLabel(r VideoRef) string {
	ident := ResourceIdentityLabel(r.Resource)
	custom := strings.TrimSpace(r.Label)
	if custom == "" {
		return ident
	}
	parent := strings.TrimSpace(r.Resource.ParentName)
	name := stripCandidateSuffix(r.Resource.Name)
	if parent != "" && (strings.EqualFold(custom, name) || !strings.Contains(custom, parent)) {
		return ident
	}
	return custom
}

func PreferredCharacterVideoVariant(r models.Resource) string {
	if strings.TrimSpace(r.StylizedImagePath) != "" {
		return "stylized"
	}
	return "original"
}

func parentIDOfResource(r models.Resource) uint {
	if r.ParentID == nil {
		return 0
	}
	return *r.ParentID
}

// NormalizeVideoRefs drops a parent character when a costume/state child is
// also referenced, forces 非真人 for characters that have it, and rewrites
// identity labels so the video model does not treat 换装图 as a new person.
func NormalizeVideoRefs(refs []VideoRef) []VideoRef {
	childParents := map[uint]bool{}
	for _, r := range refs {
		if r.Kind != "character" && r.Resource.Type != "character" {
			continue
		}
		if pid := parentIDOfResource(r.Resource); pid > 0 {
			childParents[pid] = true
		}
	}
	out := make([]VideoRef, 0, len(refs))
	for _, r := range refs {
		isChar := r.Kind == "character" || r.Resource.Type == "character"
		if isChar && childParents[r.Resource.ID] {
			continue
		}
		if isChar {
			r.Variant = PreferredCharacterVideoVariant(r.Resource)
		}
		r.Label = VideoRefIdentityLabel(r)
		out = append(out, r)
	}
	return out
}
