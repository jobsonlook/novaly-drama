package crew

import (
	"strings"

	"novaly/backend/services"
)

func fillAssetParentNames(assets []AssetItem) {
	byID := map[uint]string{}
	byName := map[string]string{}
	for _, a := range assets {
		name := strings.TrimSpace(a.Name)
		if a.ResourceID > 0 && name != "" && a.ParentID == 0 && !a.IsDerivative {
			byID[a.ResourceID] = name
		}
		if name != "" && a.ParentID == 0 && !a.IsDerivative {
			byName[strings.ToLower(name)] = name
		}
	}
	for i := range assets {
		if strings.TrimSpace(assets[i].ParentName) != "" {
			continue
		}
		if assets[i].ParentID > 0 {
			if n := strings.TrimSpace(byID[assets[i].ParentID]); n != "" {
				assets[i].ParentName = n
			}
			continue
		}
		if !assets[i].IsDerivative {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(assets[i].ParentName))
		if n := strings.TrimSpace(byName[key]); n != "" {
			assets[i].ParentName = n
		}
	}
}

// AssetDisplayName is the roster name the storyboard agent must copy into characterNames.
func AssetDisplayName(a AssetItem) string {
	name := strings.TrimSpace(a.Name)
	parent := strings.TrimSpace(a.ParentName)
	if parent == "" || (!a.IsDerivative && a.ParentID == 0) {
		return name
	}
	if strings.Contains(name, parent) {
		return name
	}
	return parent + " · " + name
}

func AssetNameMatches(a AssetItem, query string) bool {
	if services.ResourceQueryMatches(a.Name, a.ParentName, query) {
		return true
	}
	return services.ResourceQueryMatches(AssetDisplayName(a), a.ParentName, query)
}

func formatAssetRoster(assets []AssetItem) string {
	fillAssetParentNames(assets)
	var b strings.Builder
	for _, a := range assets {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(a.Type)
		b.WriteString(" | ")
		b.WriteString(AssetDisplayName(a))
		if a.IsDerivative || a.ParentID > 0 {
			parent := strings.TrimSpace(a.ParentName)
			if parent == "" {
				parent = "父角色"
			}
			b.WriteString(" | 换装/状态衍生，本场是这个外观时 characterNames 只写这一条完整名，不要同时再写「")
			b.WriteString(parent)
			b.WriteString("」；script 里人名仍用「")
			b.WriteString(parent)
			b.WriteString("」")
		}
		b.WriteString("\n")
	}
	return b.String()
}
