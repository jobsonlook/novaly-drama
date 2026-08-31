package crew

import "strings"

type shotEra int

const (
	shotEraNeutral shotEra = iota
	shotEraModern
	shotEraAncient
)

var ancientShotSignals = []string{"古代", "古装", "宫廷", "皇宫", "宫中", "御膳房", "膳房", "监栏院", "王府", "皇帝", "皇上", "郡王", "公公", "太监", "宫女", "侍卫", "奴才", "谋逆", "千叟宴"}
var modernShotSignals = []string{"现代", "都市", "办公室", "公司", "手机", "电脑", "汽车", "地铁", "西装", "衬衫", "拳馆", "拳场", "擂台", "酒吧", "会所", "包厢", "医院", "警察局"}
var ancientLookSignals = []string{"古装", "古代", "官服", "宫服", "朝服", "官袍", "宫廷", "长袍", "汉服", "唐装", "太监服", "侍卫服", "御膳房", "厨役服", "束发", "发髻"}
var modernLookSignals = []string{"现代", "都市", "西装", "衬衫", "夹克", "t恤", "T恤", "牛仔", "运动服", "拳手"}

// SelectCharacterAssetForShot resolves a character name to the most suitable
// look in that character's family. Time-travel stories therefore keep modern
// scenes modern while ancient scenes prefer an available costume derivative.
func SelectCharacterAssetForShot(assets []AssetItem, query string, shot StoryboardShot) (AssetItem, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return AssetItem{}, false
	}
	seedIndex, seedExact := -1, false
	for i, asset := range assets {
		if normalizeAssetType(asset.Type) != "character" || asset.ResourceID == 0 || !AssetNameMatches(asset, query) {
			continue
		}
		exact := sameAssetName(asset.Name, query) || sameAssetName(AssetDisplayName(asset), query)
		if seedIndex < 0 || (exact && !seedExact) {
			seedIndex, seedExact = i, exact
		}
	}
	if seedIndex < 0 {
		return AssetItem{}, false
	}

	seed := assets[seedIndex]
	identityName := strings.TrimSpace(seed.ParentName)
	rootID := seed.ParentID
	if identityName == "" {
		identityName = strings.TrimSpace(seed.Name)
	}
	if rootID == 0 && !seed.IsDerivative {
		rootID = seed.ResourceID
	}

	era := inferShotEra(shot)
	best := seed
	bestScore := characterLookScore(seed, query, shot, era, true)
	for i, candidate := range assets {
		if i == seedIndex || normalizeAssetType(candidate.Type) != "character" || candidate.ResourceID == 0 || !sameCharacterFamily(candidate, rootID, identityName) {
			continue
		}
		if score := characterLookScore(candidate, query, shot, era, false); score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best, true
}

func sameCharacterFamily(asset AssetItem, rootID uint, identityName string) bool {
	if rootID > 0 && (asset.ResourceID == rootID || asset.ParentID == rootID) {
		return true
	}
	assetIdentity := strings.TrimSpace(asset.ParentName)
	if assetIdentity == "" && !asset.IsDerivative && asset.ParentID == 0 {
		assetIdentity = strings.TrimSpace(asset.Name)
	}
	return assetIdentity != "" && strings.EqualFold(assetIdentity, strings.TrimSpace(identityName))
}

func characterLookScore(asset AssetItem, query string, shot StoryboardShot, era shotEra, selected bool) int {
	look := strings.Join([]string{asset.Name, asset.Description, asset.Prompt}, "\n")
	context := strings.Join([]string{shot.Label, shot.SceneName, shot.Script}, "\n")
	isDerivative := asset.IsDerivative || asset.ParentID > 0
	score := 0
	if selected {
		score += 20
	}
	if sameAssetName(asset.Name, query) || sameAssetName(AssetDisplayName(asset), query) {
		score += 30
	}
	switch era {
	case shotEraAncient:
		ancient, modern := keywordHits(look, ancientLookSignals), keywordHits(look, modernLookSignals)
		score += ancient*100 - modern*100
		if isDerivative && ancient > 0 {
			score += 80
		} else if isDerivative && ancient == 0 {
			score -= 15
		}
	case shotEraModern:
		score += keywordHits(look, modernLookSignals)*100 - keywordHits(look, ancientLookSignals)*120
		if !isDerivative {
			score += 50
		}
	case shotEraNeutral:
		if !isDerivative {
			score += 5
		}
	}
	// Explicit shot states such as 赤膊/战损 outrank a generic era preference.
	for _, token := range lookStateTokens(asset.Name) {
		if strings.Contains(context, token) {
			score += 160
		}
	}
	return score
}

func inferShotEra(shot StoryboardShot) shotEra {
	text := strings.Join([]string{shot.Label, shot.SceneName, shot.Script}, "\n")
	ancient, modern := keywordHits(text, ancientShotSignals), keywordHits(text, modernShotSignals)
	if ancient > modern && ancient > 0 {
		return shotEraAncient
	}
	if modern > ancient && modern > 0 {
		return shotEraModern
	}
	return shotEraNeutral
}

func keywordHits(text string, words []string) int {
	hits := 0
	for _, word := range words {
		if strings.Contains(text, word) {
			hits++
		}
	}
	return hits
}

func lookStateTokens(name string) []string {
	tokens := make([]string, 0, 4)
	for _, token := range []string{"赤膊", "战损", "受伤", "湿身", "夜行", "囚服", "婚服", "丧服"} {
		if strings.Contains(name, token) {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func sameAssetName(a, b string) bool {
	normalize := func(s string) string {
		r := strings.NewReplacer(" ", "", "·", "", "（", "", "）", "", "(", "", ")", "")
		return strings.ToLower(r.Replace(strings.TrimSpace(s)))
	}
	return normalize(a) == normalize(b)
}
