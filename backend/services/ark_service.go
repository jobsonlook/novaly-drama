package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"novaly/backend/models"
)

type ArkService struct {
	Client        *http.Client // default (Ark / Doubao / general downloads)
	PixClient     *http.Client // PixAPI API + overseas result downloads (optional proxy)
	PixAssetRelay string       // e.g. http://43.133.196.27:9080/r2 — rewrites r2.pixapi.ai downloads
}

func NewArkService(pixHTTPProxy, pixAssetRelay string) *ArkService {
	// Image generation (esp. img2img with refs) can take several minutes before headers return.
	// Disable env HTTP(S)_PROXY for Ark/Doubao so localhost uploads are not routed through a proxy
	// (a common cause of multipart i/o timeouts against doubao-web-api on 127.0.0.1).
	client := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 nil,
			MaxIdleConns:          32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 10 * time.Minute,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	pixClient := client
	if proxyURL := strings.TrimSpace(pixHTTPProxy); proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf("PIXAPI_HTTP_PROXY invalid (%s): %v — PixAPI uses direct client", proxyURL, err)
		} else {
			pixClient = &http.Client{
				Timeout: 10 * time.Minute,
				Transport: &http.Transport{
					Proxy:                 http.ProxyURL(u),
					ProxyConnectHeader:    http.Header{},
					MaxIdleConns:          32,
					IdleConnTimeout:       90 * time.Second,
					TLSHandshakeTimeout:   30 * time.Second,
					ResponseHeaderTimeout: 10 * time.Minute,
					ExpectContinueTimeout: 1 * time.Second,
				},
			}
			log.Printf("PixAPI HTTP client using proxy %s", u.Redacted())
		}
	}
	relay := strings.TrimSuffix(strings.TrimSpace(pixAssetRelay), "/")
	if relay != "" {
		log.Printf("PixAPI asset relay: %s", relay)
	}
	return &ArkService{Client: client, PixClient: pixClient, PixAssetRelay: relay}
}

func (s *ArkService) httpClient(provider models.AIProvider) *http.Client {
	if IsPixAPI(provider) {
		if s.PixClient != nil {
			return s.PixClient
		}
	}
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

func (s *ArkService) downloadClient(preferPix bool) *http.Client {
	if preferPix && s.PixClient != nil {
		return s.PixClient
	}
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

type VideoInput struct {
	Script          string
	VisualStyle     string
	ImageRefs       string
	Style           string
	LookPack        string // Toonflow Seedance 风格包；优先于 VisualStyle / Style
	Ratio           string
	Duration        int
	Resolution      string
	Refs            []VideoRef
	CharacterVoices []CharacterVoice
	Storage         *Storage
}

type CharacterVoice struct {
	Name   string
	Prompt string
}

// Max reference images per provider family; Xais video caps at 8, PixApi similar.
// Ark/Doubao doesn't publish a hard limit, so we don't cap there (user confirmed).
func maxVideoRefImages(provider models.AIProvider) int {
	if IsXais(provider) || IsPixAPI(provider) {
		return 8
	}
	return 0 // 0 = no cap (Ark/Doubao)
}

// Priority: motion-grid storyboard → transition frame (continuity anchor) →
// characters/props (identity) → scenes (environment).
func videoRefPriority(r VideoRef) int {
	gen := strings.TrimSpace(r.Resource.GenType)
	if strings.EqualFold(gen, "motion_grid") {
		return 0
	}
	if isPositioningVideoRef(r) {
		return 1
	}
	if strings.EqualFold(gen, "transition_frame") {
		return 2
	}
	switch r.Kind {
	case "character":
		return 3
	case "prop":
		return 4
	case "other":
		return 5
	case "scene":
		return 6
	}
	return 7
}

// capVideoRefs trims refs to the provider's image limit, always keeping the motion-grid
// storyboard and character identity before scene frames.
func capVideoRefs(provider models.AIProvider, refs []VideoRef) []VideoRef {
	max := maxVideoRefImages(provider)
	if max <= 0 || len(refs) <= max {
		return refs
	}
	ordered := make([]VideoRef, len(refs))
	copy(ordered, refs)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := videoRefPriority(ordered[i]), videoRefPriority(ordered[j])
		if pi == pj {
			return false // keep original relative order
		}
		return pi < pj
	})
	return ordered[:max]
}

type VideoRef struct {
	Resource models.Resource
	Kind     string
	Variant  string
	Label    string // optional custom alias for 图N为xxx
}

type VideoCharacter struct {
	Resource models.Resource
	Variant  string
}

type refImage struct {
	Index      int
	Label      string
	Path       string
	Kind       string
	Name       string
	Variant    string
	ResourceID uint
	ParentID   uint
	GenType    string
}

type VideoRefPreview struct {
	Index   int    `json:"index"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Variant string `json:"variant,omitempty"`
}

type VideoPromptPreview struct {
	Prompt     string            `json:"prompt"`
	RefImages  []VideoRefPreview `json:"refImages"`
	ModelID    string            `json:"modelId"`
	ModelName  string            `json:"modelName"`
	Ratio      string            `json:"ratio"`
	Duration   int               `json:"duration"`
	Resolution string            `json:"resolution"`
}

func PreviewVideoRequest(model models.AIModel, input VideoInput) VideoPromptPreview {
	input.Refs = NormalizeVideoRefs(input.Refs)
	refs := collectRefImages(input)
	prompt := BuildVideoPrompt(input)
	ratio := input.Ratio
	if ratio == "" {
		ratio = "16:9"
	}
	duration := input.Duration
	if duration <= 0 {
		duration = 10
	}
	if duration > 30 {
		duration = 30
	}
	resolution := input.Resolution
	if resolution == "" {
		resolution = "720p"
	}
	refPreviews := make([]VideoRefPreview, 0, len(refs))
	for _, ref := range refs {
		refPreviews = append(refPreviews, VideoRefPreview{
			Index: ref.Index, Label: ref.Label, Kind: ref.Kind, Name: ref.Name, Variant: ref.Variant,
		})
	}
	return VideoPromptPreview{
		Prompt: prompt, RefImages: refPreviews, ModelID: model.ModelID, ModelName: model.Name,
		Ratio: ratio, Duration: duration, Resolution: resolution,
	}
}

func (s *ArkService) GenerateVideo(provider models.AIProvider, model models.AIModel, input VideoInput) (string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return "", "", errors.New("请先在设置中心填写 API Key")
	}
	if strings.TrimSpace(input.Script) == "" {
		return "", "", errors.New("请先填写分镜文案")
	}
	if input.Duration <= 0 {
		input.Duration = 10
	}
	if input.Duration > 30 {
		input.Duration = 30
	}
	input.Refs = NormalizeVideoRefs(input.Refs)
	refs := collectRefImages(input)
	prompt := BuildVideoPrompt(input)
	preview := PreviewVideoRequest(model, input)
	if len(input.Refs) > 0 {
		// Trim to the provider's image budget before upload; log what got dropped.
		kept := capVideoRefs(provider, input.Refs)
		if len(kept) < len(input.Refs) {
			dropped := make([]string, 0, len(input.Refs)-len(kept))
			keepID := map[uint]bool{}
			for _, k := range kept {
				keepID[k.Resource.ID] = true
			}
			for _, r := range input.Refs {
				if !keepID[r.Resource.ID] {
					dropped = append(dropped, fmt.Sprintf("%d:%s", r.Resource.ID, r.Resource.Name))
				}
			}
			log.Printf("video refs capped for %s: %d → %d, dropped: %s", provider.Slug, len(input.Refs), len(kept), strings.Join(dropped, "; "))
			input.Refs = kept
			refs = collectRefImages(input)
			prompt = BuildVideoPrompt(input)
			preview = PreviewVideoRequest(model, input)
		}
	}
	log.Printf("\n========== VIDEO PROMPT ==========\nprovider: %s\nmodel: %s (%s)\nratio: %s | duration: %ds | resolution: %s\nrefs: %d\n--- prompt ---\n%s\n==================================\n",
		provider.Slug, preview.ModelName, preview.ModelID, preview.Ratio, preview.Duration, preview.Resolution, len(refs), prompt)
	for _, ref := range preview.RefImages {
		log.Printf("  图%d [%s] %s", ref.Index, ref.Kind, ref.Label)
	}
	content, err := s.buildVideoContent(provider, input, refs, prompt)
	if err != nil {
		return "", "", err
	}
	ratio := input.Ratio
	if ratio == "" {
		ratio = "16:9"
	}
	duration := input.Duration
	if duration <= 0 {
		duration = 10
	}
	if duration > 30 {
		duration = 30
	}
	resolution := input.Resolution
	if resolution == "" {
		resolution = "720p"
	}
	body := map[string]any{
		"model":    model.ModelID,
		"content":  content,
		"ratio":    ratio,
		"duration": duration,
	}
	if !IsDoubaoWebAPI(provider) {
		body["resolution"] = resolution
		body["generate_audio"] = true
		body["watermark"] = false
	}
	taskID, err := s.createVideoTask(provider, body)
	if err != nil {
		return "", "", humanizeVideoError(err)
	}
	// Caller should persist taskID then call WaitVideoTask so restarts can resume.
	return taskID, "", nil
}

// StartVideoTask creates a video generation task and returns its id without waiting.
func (s *ArkService) StartVideoTask(provider models.AIProvider, model models.AIModel, input VideoInput) (string, error) {
	taskID, _, err := s.GenerateVideo(provider, model, input)
	return taskID, err
}

// WaitVideoTask polls a previously created video task until it succeeds or fails.
func (s *ArkService) WaitVideoTask(provider models.AIProvider, taskID string, onETA func(string)) (string, error) {
	return s.waitVideoTask(provider, taskID, onETA)
}

// buildVideoContent assembles the text + ordered reference images for the video model.
// When a motion-grid is present, we put it as the FIRST reference image so the model treats it
// as the primary storyboard to follow; characters/scenes keep identity after that.
func (s *ArkService) buildVideoContent(provider models.AIProvider, input VideoInput, refs []refImage, prompt string) ([]map[string]any, error) {
	content := []map[string]any{{"type": "text", "text": prompt}}
	for _, ref := range refs {
		var imageURL string
		var err error
		if IsDoubaoWebAPI(provider) {
			imageURL, err = s.uploadRefImage(provider, input.Storage, ref.Path)
		} else {
			imageURL, err = input.Storage.ImageDataURL(ref.Path)
		}
		if err != nil {
			return nil, err
		}
		role := "reference_image"
		if IsDoubaoWebAPI(provider) {
			role = "reference"
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": imageURL},
			"role":      role,
		})
	}
	return content, nil
}

func (s *ArkService) uploadRefImage(provider models.AIProvider, storage *Storage, path string) (string, error) {
	data, err := storage.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取参考图失败：%w", err)
	}
	filename := filepath.Base(path)
	if filename == "" || filename == "." {
		filename = "ref.jpg"
	}
	// Shrink multi‑MB refs so doubao-web-api multipart parse stays well under ReadTimeout.
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if compressed, _, cerr := compressImageForXais(data, ext); cerr == nil && len(compressed) > 0 && len(compressed) < len(data) {
		data = compressed
		filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".jpg"
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", err
	}
	if err = w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(provider.BaseURL, "/")+"/files/uploads", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	s.setAuth(req, provider)
	resp, err := s.httpClient(provider).Do(req)
	if err != nil {
		return "", fmt.Errorf("上传参考图失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return "", parseArkError(resp.StatusCode, raw)
	}
	var out struct {
		URI string `json:"uri"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.URI == "" {
		return "", errors.New("上传参考图未返回 uri")
	}
	return out.URI, nil
}

// NoLogoConstraint is appended to every image/video generation prompt.
const NoLogoConstraint = "不要logo"

// VideoTextConstraint is appended to every video prompt: video models render
// pseudo-Chinese glyphs on signage/scrolls/banners unless told otherwise, so
// readable text is banned unless the script explicitly specifies its content.
const VideoTextConstraint = "【画面文字·最终约束】默认不生成任何文字、字母、数字或符号。唯一例外：当前分镜明确要求在剧情物体或场景表面显示、刻写、聚成的具体文字（如剑身铭文、牌匾、书信），必须按分镜指定的原字、载体、位置与时机呈现，不得改字、增字或移成字幕；未指定内容的表面保持无字纹理。剧情内文字不是口播台词，不得因其被引号包裹就朗读。严禁自动字幕、花字、对白条、歌词条、水印；所有对白只存在于音轨，禁止把对白烧录到画面。"

// SceneNoTextConstraint reminds the model that scene reference images carry no text
// and are empty plates (Toonflow art_scene X5): extras baked into a scene ref leak into video.
const SceneNoTextConstraint = "【场景参考图】场景/建筑/环境参考图是无文字、无人的空镜底板；复刻其材质、色彩、机位，不复制参考图中的意外文字或人影。当前分镜明确指定的剧情内文字可在指定载体上生成，除此之外保持无字纹理。人物只来自角色参考图。"

// SceneEmptyConstraint is appended to every scene image prompt (Toonflow: 严禁人物/人影/人体轮廓).
const SceneEmptyConstraint = "【空镜底板】画面中严禁出现任何人物、人影、人体轮廓或剪影。"

// SceneFloorPlanConstraint forces CAD line-art overhead plans. Seedream otherwise
// copies photoreal scene refs into night/stone empty plates.
const SceneFloorPlanConstraint = "【最高优先级·CAD平面图】输出必须是正交俯视二维建筑平面线稿：纯白（或浅灰）背景、黑色细实线、无透视、无消失点、无光影、无材质贴图、无夜景、无电影调色、无写实照片、无3D渲染、无室内透视图、无立面。参考图只用来推断墙/门/家具的平面相对位置与比例；禁止复刻参考图的实景外观、色调与光影。若画面看起来像实景空镜、概念图或电影截图即为失败。"

func IsSceneFloorPlanJob(name, prompt string) bool {
	blob := name + "\n" + prompt
	return strings.Contains(blob, "二维建筑平面布局图") ||
		strings.Contains(blob, "俯视布局线稿") ||
		strings.Contains(blob, "纯正交俯视二维建筑平面") ||
		strings.Contains(blob, "建筑制图线稿")
}

func withSceneFloorPlanConstraint(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return SceneFloorPlanConstraint
	}
	if strings.Contains(p, "【最高优先级·CAD平面图】") {
		return p
	}
	return SceneFloorPlanConstraint + "\n" + p + "\n" + SceneFloorPlanConstraint
}

// VideoCrowdLockConstraint hangs whenever the shot has named character subjects and no 站位图.
const VideoCrowdLockConstraint = "【人数锁定】画面中只允许出现主体定义里的人物，每人只出现一次；禁止路人、群演、背景人影、克隆分身。"

// VideoPositioningCrowdConstraint hangs when a 站位图 is attached: extras follow the map.
const VideoPositioningCrowdConstraint = "【站位图·群体】角色参考图只锁定主体定义里的人物面容与服装。有站位示意图时，全体人数、左右站位、前后景必须按站位参考图；没有站位示意图时，才按文案九格（左前/右后等）。禁止给群演长出与焦点人物相同的脸，禁止在站位图之外再添人。"

// VideoAntiTwinConstraint is Toonflow Seedance「多主体必挂」双胞胎兜底.
const VideoAntiTwinConstraint = "【多主体·双胞胎兜底】视频全程禁止出现外形、着装、配饰完全一致的人物，禁止生成同款分身、双胞胎效果，同一画面每个主体仅保留单个对应人物。"

// VideoMultiPersonFrontalConstraint is Toonflow Seedance「多人正面动态必挂」.
const VideoMultiPersonFrontalConstraint = "【多人正面动态】明确画面左/右侧角色辨识特征（发型、服装、体型），固定机位，禁止无故跳轴或左右换位。"

const VideoAccessoryLockConstraint = "【配饰锁定】人物脖子、耳朵、手腕上的配饰以角色参考图为准：参考图没有奖牌/项链，成片禁止添加；参考图有的配饰必须同款同色同位置，禁止换成另一枚奖牌。剧本中的奖牌、奖杯若绑定了道具参考图，按道具图生成（拿着、看着、挂着），不要改成戴在脖子上的项链。"

// VideoQualityPack is Toonflow Seedance 第三段的画质/稳定包（不含字幕与水印，那些由 VideoTextConstraint / withNoLogo 承担）。
const VideoQualityPack = "高清，细节丰富，电影质感；人物面部稳定不变形、五官清晰、动作连贯自然，不僵硬，无穿模无卡顿"

const CharacterStylizePrompt = "只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改"
const SceneStylizePrompt = "只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改"
const OtherStylizePrompt = "只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改"

// ImageRefMeta labels one attached reference for 图N legends on resource img2img.
type ImageRefMeta struct {
	Label string
	Kind  string // character | scene | prop | other
}

const ResourceImageRefFusionConstraint = "【多参考图分工】角色参考图锁定人物身份、面容、体型、服装与四视图布局；道具参考图只用于把该物件替换到人物身上的正确位置（奖牌/项链戴脖子，手表戴手腕），面部特写与全身三视图都必须带上。禁止把道具图当成人物底模，禁止只临摹人物图而丢掉道具。"

func BuildResourceImageRefLegend(refs []ImageRefMeta) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	hasChar, hasProp := false, false
	for i, r := range refs {
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = fmt.Sprintf("参考%d", i+1)
		}
		kind := "参考"
		switch strings.TrimSpace(r.Kind) {
		case "character":
			kind = "角色"
			hasChar = true
		case "scene":
			kind = "场景"
		case "prop":
			kind = "道具"
			hasProp = true
		}
		parts = append(parts, fmt.Sprintf("图%d为%s（%s）", i+1, label, kind))
	}
	legend := "参考图：" + strings.Join(parts, "，") + "。"
	if hasChar && hasProp {
		return legend + "\n" + ResourceImageRefFusionConstraint
	}
	if len(refs) > 1 {
		return legend + "\n按图号引用上方参考图，不要弄混。"
	}
	return legend
}

func PrependImageRefLegend(prompt, legend string) string {
	prompt = strings.TrimSpace(prompt)
	legend = strings.TrimSpace(legend)
	if legend == "" {
		return prompt
	}
	if strings.Contains(prompt, "参考图：图") {
		return prompt
	}
	if prompt == "" {
		return legend
	}
	return legend + "\n\n" + prompt
}

var resourceImageRefLegendRE = regexp.MustCompile(`参考图：图\d为[\s\S]*?。(?:\s*按图号引用上方参考图，不要弄混。)?`)

// StripImageRefLegend removes auto-inserted "参考图：图1为…" blocks so they cannot
// remap 图1/图2 inside reverse/skeleton prompts.
func StripImageRefLegend(s string) string {
	s = resourceImageRefLegendRE.ReplaceAllStringFunc(s, func(m string) string {
		if strings.Contains(m, "反打空间线稿") || strings.Contains(m, "反打镜头线稿") {
			return m
		}
		return ""
	})
	s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	return strings.TrimSpace(s)
}

func withNoLogo(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return NoLogoConstraint
	}
	if strings.Contains(p, NoLogoConstraint) {
		return p
	}
	return p + "\n" + NoLogoConstraint
}

func withSceneEmptyConstraint(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return SceneEmptyConstraint
	}
	if strings.Contains(p, "【空镜底板】") {
		return p
	}
	return p + "\n" + SceneEmptyConstraint
}

func (s *ArkService) stylizeImage(provider models.AIProvider, model models.AIModel, sourceDataURL, prompt, resolution string) (string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return "", errors.New("请先在设置中心填写 API Key")
	}
	prompt = withNoLogo(prompt)
	// Resource images use a landscape canvas throughout the asset pipeline.
	// Keep stylized variants at the same 16:9 ratio instead of turning them
	// into square images during image-to-image conversion.
	spec := ImageGenSpec{Quality: resolution, Resolution: resolution, Aspect: "16:9"}
	if IsPixAPI(provider) || IsXais(provider) {
		return s.generateImageWithReferences(provider, model, prompt, []string{sourceDataURL}, 0, spec)
	}
	body := map[string]any{
		"model":                       model.ModelID,
		"prompt":                      prompt,
		"image":                       sourceDataURL,
		"size":                        resolveArkImageSize(effectiveImageResolution(spec), spec.Aspect),
		"response_format":             "url",
		"watermark":                   false,
		"sequential_image_generation": "disabled",
	}
	raw, err := s.post(provider, "/images/generations", body)
	if err != nil {
		return "", err
	}
	var out struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 || out.Data[0].URL == "" {
		return "", errors.New("模型未返回非真人图地址")
	}
	return out.Data[0].URL, nil
}

func (s *ArkService) StylizeImage(provider models.AIProvider, model models.AIModel, sourceDataURL, prompt string) (string, error) {
	return s.stylizeImage(provider, model, sourceDataURL, prompt, "1k")
}

func (s *ArkService) StylizeImageWithSpec(provider models.AIProvider, model models.AIModel, sourceDataURL, prompt, resolution string) (string, error) {
	return s.stylizeImage(provider, model, sourceDataURL, prompt, resolution)
}

func (s *ArkService) StylizeCharacterImage(provider models.AIProvider, model models.AIModel, sourceDataURL string) (string, error) {
	return s.stylizeImage(provider, model, sourceDataURL, CharacterStylizePrompt, "1k")
}

func (s *ArkService) StylizeSceneImage(provider models.AIProvider, model models.AIModel, sourceDataURL string) (string, error) {
	return s.stylizeImage(provider, model, sourceDataURL, SceneStylizePrompt, "1k")
}

func characterRefImagePath(c models.Resource, variant string) string {
	if variant == "original" {
		return c.ImagePath
	}
	if c.StylizedImagePath != "" {
		return c.StylizedImagePath
	}
	return c.ImagePath
}

func sceneRefImagePath(s models.Resource, variant string) string {
	if variant == "original" {
		return s.ImagePath
	}
	if s.StylizedImagePath != "" {
		return s.StylizedImagePath
	}
	return s.ImagePath
}

func otherRefImagePath(r models.Resource, variant string) string {
	if variant == "original" {
		return r.ImagePath
	}
	if r.StylizedImagePath != "" {
		return r.StylizedImagePath
	}
	return r.ImagePath
}

func collectRefImages(input VideoInput) []refImage {
	input.Refs = NormalizeVideoRefs(input.Refs)
	refs := make([]refImage, 0, len(input.Refs))
	idx := 1
	for _, r := range input.Refs {
		var path string
		name := r.Resource.Name
		variant := r.Variant
		if r.Kind == "character" {
			path = characterRefImagePath(r.Resource, variant)
		} else if r.Kind == "scene" {
			if variant == "" {
				variant = "original"
			}
			path = sceneRefImagePath(r.Resource, variant)
		} else if r.Kind == "other" {
			if variant == "" {
				variant = "stylized"
			}
			path = otherRefImagePath(r.Resource, variant)
		} else if r.Kind == "prop" {
			path = r.Resource.ImagePath
		}
		if path == "" {
			continue
		}
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = ResourceIdentityLabel(r.Resource)
		}
		if label == "" {
			label = strings.TrimSpace(name)
		}
		if r.Kind == "scene" && label != "" && !strings.Contains(label, "无文字") && !strings.EqualFold(strings.TrimSpace(r.Resource.GenType), "transition_frame") {
			label = label + "（无文字）"
		}
		if label == "" {
			label = fmt.Sprintf("图%d", idx)
		}
		parentID := parentIDOfResource(r.Resource)
		refs = append(refs, refImage{
			Index: idx, Label: label, Path: path, Kind: r.Kind, Name: name, Variant: variant,
			ResourceID: r.Resource.ID, ParentID: parentID, GenType: strings.TrimSpace(r.Resource.GenType),
		})
		idx++
	}
	return refs
}

func buildVideoRefLegend(refs []refImage) string {
	if len(refs) == 0 {
		return ""
	}
	subjectN, sceneN, propN := 1, 1, 1
	idIndex := map[uint]int{}
	for _, r := range refs {
		if r.ResourceID > 0 {
			idIndex[r.ResourceID] = r.Index
		}
	}
	parts := make([]string, 0, len(refs))
	same := make([]string, 0)
	for _, r := range refs {
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = strings.TrimSpace(r.Name)
		}
		if label == "" {
			continue
		}
		switch {
		case isPositioningRefImage(r):
			parts = append(parts, fmt.Sprintf("将图%d定义为站位示意图（%s）", r.Index, label))
		case r.Kind == "character":
			if CharacterLooksLikeCrowd(label) {
				parts = append(parts, fmt.Sprintf("将图%d定义为群演外观参考（%s；不是新增主体，人数与站位以站位图为准）", r.Index, label))
				continue
			}
			tag := fmt.Sprintf("<主体%d>", subjectN)
			subjectN++
			parts = append(parts, fmt.Sprintf("将图%d中的人物定义为%s（%s）", r.Index, tag, label))
			if r.ParentID > 0 {
				if pi, ok := idIndex[r.ParentID]; ok {
					same = append(same, fmt.Sprintf("图%d与图%d是同一人，图%d为换装/状态，本镜以图%d为准，不要把图%d当成另一个人", r.Index, pi, r.Index, r.Index, pi))
				}
			}
		case r.Kind == "scene":
			tag := fmt.Sprintf("<场景%d>", sceneN)
			sceneN++
			parts = append(parts, fmt.Sprintf("将图%d定义为%s（%s）", r.Index, tag, label))
		default:
			tag := fmt.Sprintf("<道具%d>", propN)
			propN++
			parts = append(parts, fmt.Sprintf("将图%d定义为%s（%s）", r.Index, tag, label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := "主体定义：" + strings.Join(parts, "；") + "。"
	if len(same) > 0 {
		out += strings.Join(same, "；") + "。"
	}
	out += "换装/战损图不是新角色，必须认成括号里的人名。"
	charN := distinctVideoCharacterCount(refs)
	sceneNCount := 0
	hasPos := false
	for _, r := range refs {
		if isPositioningRefImage(r) {
			hasPos = true
			continue
		}
		if r.Kind == "scene" {
			sceneNCount++
		}
	}
	if hasPos {
		out += "站位示意图决定全体人数、左右站位、前后景与整体构图，必须按参考图出画；没有站位示意图时才按文案九格。角色参考图只锁定主体定义里的人物面容服装。群演和未单独定义的角色只按站位图出画，禁止再长出一张与焦点人物相同的脸。"
	} else if charN > 0 {
		out += fmt.Sprintf("本镜出场人物共%d人，只允许上述主体入画，每人只出现一次，禁止路人、群演、同款分身。", charN)
	} else if sceneNCount > 0 {
		out += "本镜无角色主体，场景按空镜底板处理，禁止出现任何人物、人影。"
	}
	if sceneNCount > 0 && charN > 0 {
		out += "场景参考图是空镜底板，不要从图中抄人。"
	}
	return out
}

func isPositioningRefImage(r refImage) bool {
	if strings.EqualFold(strings.TrimSpace(r.GenType), "positioning") {
		return true
	}
	return strings.Contains(r.Name, "站位") || strings.Contains(r.Label, "站位")
}

func isPositioningVideoRef(r VideoRef) bool {
	if strings.EqualFold(strings.TrimSpace(r.Resource.GenType), "positioning") {
		return true
	}
	return strings.Contains(r.Resource.Name, "站位") || strings.Contains(r.Label, "站位")
}

func characterVideoIdentityRoot(r VideoRef) uint {
	if pid := parentIDOfResource(r.Resource); pid > 0 {
		return pid
	}
	return r.Resource.ID
}

func capNamedCharacterVideoRefs(refs []VideoRef, script string) []VideoRef {
	drop := map[uint]bool{}
	for _, r := range refs {
		if r.Kind != "character" && r.Resource.Type != "character" {
			continue
		}
		if isPositioningVideoRef(r) {
			continue
		}
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = strings.TrimSpace(r.Resource.Name)
		}
		if CharacterLooksLikeCrowd(label) {
			drop[characterVideoIdentityRoot(r)] = true
		}
	}
	if len(drop) == 0 {
		return refs
	}
	out := make([]VideoRef, 0, len(refs))
	for _, r := range refs {
		if r.Kind != "character" && r.Resource.Type != "character" {
			out = append(out, r)
			continue
		}
		if isPositioningVideoRef(r) {
			out = append(out, r)
			continue
		}
		if drop[characterVideoIdentityRoot(r)] {
			continue
		}
		out = append(out, r)
	}
	return out
}

func distinctVideoCharacterCount(refs []refImage) int {
	ids := map[uint]struct{}{}
	anon := 0
	for _, r := range refs {
		if r.Kind != "character" {
			continue
		}
		root := r.ResourceID
		if r.ParentID > 0 {
			root = r.ParentID
		}
		if root > 0 {
			ids[root] = struct{}{}
		} else {
			anon++
		}
	}
	return len(ids) + anon
}

// VideoPositioningAnnotationConstraint tells the video model to ignore schematic labels
// (mosaic faces / name tags) baked into 站位图 reference images.
const VideoPositioningAnnotationConstraint = "重要：参考图上若有遮挡脸的色块、名字贴纸或白底名牌，只是站位标记，不是服装或道具。成片面容按角色参考图，不要把贴纸和名牌画进成片；服装保持干净无文字。"

func hasSceneVideoRef(input VideoInput) bool {
	for _, r := range input.Refs {
		if r.Kind == "scene" && !isPositioningVideoRef(r) {
			return true
		}
	}
	return false
}

func hasCharacterVideoRef(input VideoInput) bool {
	for _, r := range input.Refs {
		if r.Kind == "character" || r.Resource.Type == "character" {
			return true
		}
	}
	return false
}

func hasPositioningVideoRef(input VideoInput) bool {
	for _, r := range input.Refs {
		if isPositioningVideoRef(r) {
			return true
		}
		gen := strings.TrimSpace(r.Resource.GenType)
		if strings.EqualFold(gen, "transition_frame") {
			return true
		}
		if strings.Contains(r.Resource.Name, "尾帧") || strings.Contains(r.Label, "尾帧") {
			return true
		}
	}
	return false
}

func BuildVideoPrompt(input VideoInput) string {
	input.Refs = NormalizeVideoRefs(input.Refs)
	parts := make([]string, 0, 8)
	refs := collectRefImages(input)
	legend := buildVideoRefLegend(refs)
	if legend != "" {
		parts = append(parts, legend)
	} else if imageRefs := strings.TrimSpace(input.ImageRefs); imageRefs != "" {
		parts = append(parts, imageRefs)
	}
	parts = append(parts, VideoNoSubtitleConstraint)
	hasSpeech := false
	if s := strings.TrimSpace(input.Script); s != "" {
		// UI 用 @图1，豆包侧认 图1；统一一份方便模型对齐参考图
		s = strings.ReplaceAll(s, "@图", "图")
		s = stripVideoSubtitleDirectives(s)
		s = clipScriptToDuration(s, input.Duration)
		s = SanitizePlatformViolencePreserveDialogue(s)
		s = rewriteScriptForSeedance(s, refs, input.CharacterVoices)
		parts = append(parts, s)
		hasSpeech = hasSeedanceSpeech(s)
		if hasSpeech {
			parts = append(parts, VideoSpeechConstraint)
		} else {
			parts = append(parts, VideoNoSpeechConstraint)
		}
	}
	if look := buildVideoLookLine(input); look != "" {
		parts = append(parts, look)
	}
	// Voice descriptions strongly invite video models to invent dialogue. They
	// are useful only when the shot actually contains an explicit speech clause.
	if hasSpeech {
		if voices := formatCharacterVoices(input.CharacterVoices); voices != "" {
			parts = append(parts, voices)
		}
	}
	if HasMotionGridVideoRef(input) {
		// Motion-grid rule is the highest-priority compositional constraint: sequence the video by the grid.
		parts = append(parts, VideoMotionGridConstraint)
	}
	if hasPositioningVideoRef(input) {
		parts = append(parts, VideoPositioningAnnotationConstraint)
	}
	if hasSceneVideoRef(input) {
		parts = append(parts, SceneNoTextConstraint)
	}
	if hasCharacterVideoRef(input) {
		parts = append(parts, VideoAccessoryLockConstraint)
	}
	if n := distinctVideoCharacterCount(refs); n >= 1 {
		if hasPositioningVideoRef(input) {
			parts = append(parts, VideoPositioningCrowdConstraint)
		} else {
			parts = append(parts, VideoCrowdLockConstraint)
		}
		if n >= 2 {
			parts = append(parts, VideoAntiTwinConstraint)
			parts = append(parts, VideoMultiPersonFrontalConstraint)
		}
		if hasPositioningVideoRef(input) {
			parts = append(parts, VideoSpatialBlockingFromMapConstraint)
		} else if n >= 2 {
			parts = append(parts, VideoSpatialBlockingConstraint)
		}
	}
	if guide := strings.TrimSpace(dramaSkillGuidance["video-prompts"]); guide != "" {
		parts = append(parts, guide)
	}
	parts = append(parts, VideoTextConstraint)
	ratio := strings.TrimSpace(input.Ratio)
	if ratio == "" {
		ratio = "16:9"
	}
	parts = append(parts, "视频比例为"+ratio)
	// Repeat the applicable audio rule at the tail because long multi-reference
	// prompts tend to weight their final instructions more heavily. Previously
	// only silent shots repeated their rule here, so dialogue instructions could
	// be buried by the look/reference constraints appended after the script.
	if hasSpeech {
		parts = append(parts, VideoSpeechConstraint)
	} else {
		parts = append(parts, VideoNoSpeechConstraint)
	}
	// Keep the no-subtitle rule as the final instruction as well. Some video
	// models weigh the tail of a long multi-reference prompt more heavily.
	parts = append(parts, VideoNoSubtitleConstraint)
	return SanitizePlatformViolencePreserveDialogue(withNoLogo(strings.Join(parts, "\n")))
}

func buildVideoLookLine(input VideoInput) string {
	look := strings.TrimSpace(input.LookPack)
	if look == "" {
		look = strings.TrimSpace(input.VisualStyle)
	}
	if look == "" {
		look = strings.TrimSpace(input.Style)
	}
	if look == "" {
		return "画面质感：" + VideoQualityPack
	}
	if strings.Contains(look, "人物面部稳定不变形") {
		return "画面质感：" + look
	}
	return "画面质感：" + look + "；" + VideoQualityPack
}

func formatCharacterVoices(voices []CharacterVoice) string {
	seen := map[string]bool{}
	lines := make([]string, 0, len(voices))
	for _, v := range voices {
		name := strings.TrimSpace(v.Name)
		prompt := strings.TrimSpace(v.Prompt)
		if name == "" || prompt == "" || seen[name] {
			continue
		}
		seen[name] = true
		lines = append(lines, "- "+name+"："+prompt)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【声音要求】\n" + strings.Join(lines, "\n")
}

type ImageGenProgress func(done, total int, message string)

type CharacterImageInput struct {
	Name            string
	Description     string
	Count           int
	Quality         string // low | medium | high
	Aspect          string // 1:1 | 16:9 | 9:16
	ReferenceImages []string
	LockIdentity    bool // 衍生图：锁定参考图面容/体型，只叠加状态
	RawPrompt       bool // 已是完整提示词，不要再套定妆照模板
	OnProgress      ImageGenProgress
}

func (s *ArkService) GenerateCharacterCandidates(provider models.AIProvider, model models.AIModel, input CharacterImageInput) ([]string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return nil, "", errors.New("请先在设置中心填写 API Key")
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, "", errors.New("请填写角色名称")
	}
	if strings.TrimSpace(input.Description) == "" && len(input.ReferenceImages) == 0 {
		return nil, "", errors.New("请填写角色描述或上传参考图")
	}
	count := normalizeImageCandidateCount(input.Count)
	refImages := input.ReferenceImages
	useImg2Img := len(refImages) > 0
	var prompt string
	if input.RawPrompt && strings.TrimSpace(input.Description) != "" {
		prompt = strings.TrimSpace(input.Description)
	} else if useImg2Img && input.LockIdentity {
		prompt = buildCharacterIdentityLockPrompt(input.Name, input.Description)
	} else if useImg2Img {
		prompt = buildCharacterImg2ImgPrompt(input.Name, input.Description, len(refImages))
	} else {
		prompt = buildCharacterPrompt(input.Name, input.Description)
	}
	prompt = withNoLogo(prompt)
	urls, err := s.generateImageCandidates(provider, model, prompt, refImages, count, 1000, input.OnProgress, ImageGenSpec{
		Quality: input.Quality, Resolution: input.Quality, Aspect: input.Aspect,
	})
	if err != nil {
		return nil, prompt, err
	}
	return urls, prompt, nil
}

type SceneImageInput struct {
	Name            string
	Description     string
	Style           string // project global style only; scene prompt otherwise stays verbatim
	Count           int
	Quality         string
	Aspect          string
	ReferenceImages []string
	LockIdentity    bool
	RawPrompt       bool
	OnProgress      ImageGenProgress
}

func (s *ArkService) GenerateSceneCandidates(provider models.AIProvider, model models.AIModel, input SceneImageInput) ([]string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return nil, "", errors.New("请先在设置中心填写 API Key")
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, "", errors.New("请填写场景名称")
	}
	if strings.TrimSpace(input.Description) == "" && len(input.ReferenceImages) == 0 {
		return nil, "", errors.New("请填写场景描述或上传参考图")
	}
	count := normalizeImageCandidateCount(input.Count)
	refImages := input.ReferenceImages
	var prompt string
	if input.RawPrompt && strings.TrimSpace(input.Description) != "" {
		prompt = withNoLogo(strings.TrimSpace(input.Description))
	} else if len(refImages) > 0 && input.LockIdentity {
		prompt = withNoLogo(buildSceneIdentityLockPrompt(input.Description, input.Style))
	} else {
		prompt = withNoLogo(buildScenePrompt(input.Description, input.Style))
	}
	if IsSceneFloorPlanJob(input.Name, prompt) {
		// Floor-plan jobs must not inherit「空镜底板」photoreal scene framing.
		prompt = withSceneFloorPlanConstraint(prompt)
	} else {
		prompt = withSceneEmptyConstraint(prompt)
	}
	urls, err := s.generateImageCandidates(provider, model, prompt, refImages, count, 2000, input.OnProgress, ImageGenSpec{
		Quality: input.Quality, Resolution: input.Quality, Aspect: input.Aspect,
	})
	if err != nil {
		return nil, prompt, err
	}
	return urls, prompt, nil
}

type PropImageInput struct {
	Name            string
	Description     string
	Count           int
	Quality         string
	Aspect          string
	ReferenceImages []string
	RawPrompt       bool
	OnProgress      ImageGenProgress
}

func (s *ArkService) GeneratePropCandidates(provider models.AIProvider, model models.AIModel, input PropImageInput) ([]string, string, error) {
	if ProviderRequiresAPIKey(provider) && provider.APIKey == "" {
		return nil, "", errors.New("请先在设置中心填写 API Key")
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, "", errors.New("请填写道具名称")
	}
	if strings.TrimSpace(input.Description) == "" && len(input.ReferenceImages) == 0 {
		return nil, "", errors.New("请填写道具描述或上传参考图")
	}
	count := normalizeImageCandidateCount(input.Count)
	refImages := input.ReferenceImages
	useImg2Img := len(refImages) > 0
	var prompt string
	if input.RawPrompt && strings.TrimSpace(input.Description) != "" {
		prompt = strings.TrimSpace(input.Description)
	} else if useImg2Img {
		prompt = buildPropImg2ImgPrompt(input.Name, input.Description, len(refImages))
	} else {
		prompt = buildPropPrompt(input.Name, input.Description)
	}
	prompt = withNoLogo(prompt)
	urls, err := s.generateImageCandidates(provider, model, prompt, refImages, count, 3000, input.OnProgress, ImageGenSpec{
		Quality: input.Quality, Resolution: input.Quality, Aspect: input.Aspect,
	})
	if err != nil {
		return nil, prompt, err
	}
	return urls, prompt, nil
}

func normalizeImageCandidateCount(count int) int {
	if count < 1 {
		return 4
	}
	if count > 6 {
		return 6
	}
	return count
}

// ImageGenSpec controls output clarity / aspect for image APIs.
type ImageGenSpec struct {
	Quality    string // legacy: low | medium | high
	Resolution string // preferred: 1k | 2k | 4k (overrides Quality when set)
	Aspect     string // 1:1 | 16:9 | 9:16
}

func NormalizeImageResolution(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "1k", "1K", "low", "draft", "标清", "快":
		return "1k"
	case "4k", "4K", "high", "超清", "慢":
		return "4k"
	case "2k", "2K", "medium", "高清":
		return "2k"
	default:
		return "1k"
	}
}

func NormalizeImageQuality(q string) string {
	switch NormalizeImageResolution(q) {
	case "1k":
		return "low"
	case "4k":
		return "high"
	default:
		return "medium"
	}
}

func effectiveImageResolution(spec ImageGenSpec) string {
	if strings.TrimSpace(spec.Resolution) != "" {
		return NormalizeImageResolution(spec.Resolution)
	}
	if strings.TrimSpace(spec.Quality) != "" {
		return NormalizeImageResolution(spec.Quality)
	}
	return "1k"
}

func NormalizeImageAspect(aspect, resType string) string {
	switch strings.TrimSpace(resType) {
	case "character", "prop":
		// 四视图定妆照 / 道具参考必须横构图；不要跟短剧 9:16 成片比例走。
		return "16:9"
	case "scene_grid", "motion_grid", "scene_reverse_skeleton":
		// 3×3 拼图 / 反打俯视线稿整张固定 16:9。
		return "16:9"
	case "scene_panorama":
		// Dramaclaw-style equirectangular panorama.
		return "2:1"
	}
	a := strings.TrimSpace(aspect)
	switch a {
	case "1:1", "16:9", "9:16", "2:1":
		return a
	}
	if a == "3:4" {
		return "9:16"
	}
	if a == "4:3" {
		return "16:9"
	}
	return "16:9"
}

func resolvePixAPIImageParams(quality, aspect string) (q, size string) {
	res := NormalizeImageResolution(quality)
	quality = NormalizeImageQuality(res)
	aspect = NormalizeImageAspect(aspect, "")
	switch quality {
	case "low":
		q = "low"
		switch aspect {
		case "9:16":
			size = "1024x1536"
		case "16:9":
			size = "1536x1024"
		case "2:1":
			size = "2048x1024"
		default:
			size = "1024x1024"
		}
	case "high":
		q = "high"
		switch aspect {
		case "9:16":
			size = "1440x2560"
		case "16:9":
			size = "2560x1440"
		case "2:1":
			size = "2880x1440"
		default:
			size = "2048x2048"
		}
	default:
		q = "medium"
		switch aspect {
		case "9:16":
			size = "1024x1536"
		case "16:9":
			size = "2048x1152"
		case "2:1":
			size = "2880x1440"
		default:
			size = "1536x1536"
		}
	}
	return q, size
}

// resolveArkImageSize maps UI resolution + aspect to Seedream `size`.
// Seedream 5.0 Lite rejects "1K"; pass WIDTHxHEIGHT so 四视图不会跟着竖屏项目变成 9:16.
func resolveArkImageSize(quality, aspect string) string {
	a := NormalizeImageAspect(aspect, "")
	if NormalizeImageResolution(quality) == "4k" {
		switch a {
		case "9:16":
			return "2160x3840"
		case "1:1":
			return "4096x4096"
		case "2:1":
			return "3840x1920"
		default:
			return "3840x2160"
		}
	}
	switch a {
	case "9:16":
		return "1440x2560"
	case "1:1":
		return "2048x2048"
	case "2:1":
		// Seedream requires ≥3686400 pixels; 2560x1280=3276800 is rejected.
		// 2880x1440 keeps exact 2:1 and clears the floor (4147200).
		return "2880x1440"
	default:
		return "2560x1440"
	}
}

func (s *ArkService) generateImageCandidates(provider models.AIProvider, model models.AIModel, prompt string, refImages []string, count, seedBase int, onProgress ImageGenProgress, spec ImageGenSpec) ([]string, error) {
	useImg2Img := len(refImages) > 0
	type result struct {
		idx int
		url string
		err error
	}
	ch := make(chan result, count)
	for i := 0; i < count; i++ {
		go func(idx int) {
			var url string
			var err error
			if useImg2Img {
				url, err = s.generateImageWithReferences(provider, model, prompt, refImages, seedBase+idx, spec)
			} else {
				url, err = s.generateImage(provider, model, prompt, seedBase+idx, spec)
			}
			ch <- result{idx: idx, url: url, err: err}
		}(i)
	}
	ordered := make([]string, count)
	ok := 0
	var firstErr error
	for i := 0; i < count; i++ {
		r := <-ch
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		ordered[r.idx] = r.url
		ok++
		if onProgress != nil {
			onProgress(ok, count, fmt.Sprintf("AI 生成中（%d/%d）", ok, count))
		}
	}
	if ok == 0 {
		return nil, firstErr
	}
	urls := make([]string, 0, ok)
	for _, url := range ordered {
		if url != "" {
			urls = append(urls, url)
		}
	}
	return urls, nil
}

func buildPropPrompt(name, description string) string {
	return fmt.Sprintf(`专业影视道具参考图（Prop / Object Reference），纯白或中性背景产品摄影，主体清晰居中，无人物，无文字无水印。

道具名称：%s
道具外观与材质：%s

画面要求：16:9 横构图，超写实产品摄影质感，材质纹理清晰，光影自然，适合作为 AI 视频分镜的道具参考图；8K 超高细节，不是二次元插画，不是 CG 游戏图标，不是概念涂鸦`, name, description)
}

// buildScenePrompt uses the user description verbatim and only appends the project global style.
func buildScenePrompt(description, style string) string {
	parts := make([]string, 0, 2)
	if d := strings.TrimSpace(description); d != "" {
		parts = append(parts, d)
	}
	if s := strings.TrimSpace(style); s != "" {
		parts = append(parts, "画面质感："+s)
	}
	return strings.Join(parts, "\n")
}

func buildCharacterIdentityLockPrompt(name, overlay string) string {
	overlay = strings.TrimSpace(overlay)
	if overlay == "" {
		overlay = "只改变服装/妆造/状态，其余与底模一致"
	}
	return fmt.Sprintf(`图1是角色「%s」的底模定妆照。必须锁定同一人：五官、脸型、年龄、肤色、体型、头身比与图1完全一致。禁止换脸、禁止重画五官、禁止另起一个角色。不要复制图1的画幅比例。

只叠加状态差异（换装/妆造/战损/变身），不要改身份：
%s

输出必须是 16:9 横构图角色设定四视图（不是竖图）：左侧面部特写，右侧从左到右正面+侧面+背面全身。纯白背景，摄影棚均匀柔光，无文字无水印。`, name, overlay)
}

func buildSceneIdentityLockPrompt(description, style string) string {
	desc := strings.TrimSpace(description)
	parts := []string{
		"图1是该场景的底模主视图。必须保持建筑结构、空间布局、主要物件与镜头机位与图1一致，禁止重画成另一个地方。",
		"只改变时段带来的天空、灯光、色温、阴影与氛围。",
	}
	if desc != "" {
		parts = append(parts, desc)
	}
	if s := strings.TrimSpace(style); s != "" {
		parts = append(parts, "画面质感："+s)
	}
	parts = append(parts, "单画面空镜主视图，画面中无任何人物，无文字无水印。")
	return strings.Join(parts, "\n")
}

func buildCharacterImg2ImgPrompt(name, description string, refCount int) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = "保持参考图中角色的面部特征、发型、服装与气质，生成专业角色定妆照"
	}
	refHint := "基于上传的角色参考图进行图生图重绘"
	if refCount > 1 {
		refHint = fmt.Sprintf("已提供 %d 张参考图，请融合角色外貌与服装特征", refCount)
	}
	return fmt.Sprintf(`%s，生成专业影视角色设定参考图（Character Reference Sheet），16:9 横构图，纯白背景，摄影棚均匀柔光，无文字无水印。

画面布局（严格遵循）：
- 左侧：角色「%s」面部高清特写肖像，可见肩颈，中性沉稳表情
- 右侧：同一角色三张全身站姿定妆照，从左到右依次为正面、侧面、背面，服装配饰与发型完全一致

角色要求：%s

画面质感：超写实真人摄影棚拍摄，8K超高清，肤质与布料纹理真实，不是二次元插画，不是CG游戏立绘`, refHint, name, desc)
}

func buildPropImg2ImgPrompt(name, description string, refCount int) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = "保持参考图中道具的外观、材质与比例，生成专业道具参考图"
	}
	refHint := "基于上传的道具参考图进行图生图重绘"
	if refCount > 1 {
		refHint = fmt.Sprintf("已提供 %d 张参考图，请融合道具外观与材质细节", refCount)
	}
	return fmt.Sprintf(`%s，生成专业影视道具参考图，纯白或中性背景产品摄影，主体清晰居中，无人物，无文字无水印。

道具名称：%s
道具要求：%s

画面要求：16:9 横构图，超写实产品摄影质感，材质纹理清晰，适合作为 AI 视频分镜的道具参考图；8K 超高细节`, refHint, name, desc)
}

func buildCharacterPrompt(name, description string) string {
	return fmt.Sprintf(`专业影视角色设定参考图（Character Reference Sheet / Turnaround），16:9 横构图，纯白背景，摄影棚均匀柔光，无阴影干扰，无文字无水印。

画面布局（严格遵循）：
- 左侧：角色「%s」面部高清特写肖像，可见肩颈，中性沉稳表情
- 右侧：同一角色三张全身站姿定妆照，从左到右依次为正面、侧面、背面，服装配饰与发型完全一致

角色外貌与服装：%s

画面质感：超写实真人摄影棚拍摄，8K超高清，肤质与布料纹理真实，自然淡妆，光线柔和均匀，不是二次元，不是插画，不是CG，不是3D渲染，不是游戏角色立绘`, name, description)
}

func (s *ArkService) generateImage(provider models.AIProvider, model models.AIModel, prompt string, seed int, spec ImageGenSpec) (string, error) {
	return s.generateImageWithReferences(provider, model, prompt, nil, seed, spec)
}

func (s *ArkService) generateImageWithReferences(provider models.AIProvider, model models.AIModel, prompt string, referenceImages []string, seed int, spec ImageGenSpec) (string, error) {
	if IsXais(provider) {
		url, err := s.generateXaisImage(provider, model, prompt, referenceImages, spec)
		if err != nil && (isHTTPTimeout(err) || isXaisModelOverload(err)) {
			delays := []time.Duration{3 * time.Second, 8 * time.Second, 15 * time.Second}
			for i, delay := range delays {
				if err == nil || !(isHTTPTimeout(err) || isXaisModelOverload(err)) {
					break
				}
				log.Printf("xais image gen retry %d/%d after %s (%v)", i+1, len(delays), delay, err)
				time.Sleep(delay)
				url, err = s.generateXaisImage(provider, model, prompt, referenceImages, spec)
			}
		}
		// GPT Image 1K (Xais Img2_1K) is often overloaded; fall back to Image2_2K once.
		if err != nil && isXaisModelOverload(err) && isXaisGPTImageFamily(model.ModelID) && effectiveImageResolution(spec) == "1k" {
			spec2 := spec
			spec2.Resolution = "2k"
			log.Printf("xais GPT Image 1K overloaded, falling back to 2K")
			url, err = s.generateXaisImage(provider, model, prompt, referenceImages, spec2)
		}
		if err != nil {
			return "", humanizeXaisError(err)
		}
		return url, nil
	}
	path := "/images/generations"
	var body map[string]any
	if IsPixAPI(provider) {
		if len(referenceImages) > 0 {
			if err := validatePixAPIReferenceImages(referenceImages); err != nil {
				return "", err
			}
		}
		path, body = buildPixAPIImageRequest(model, prompt, referenceImages, spec)
	} else {
		body = map[string]any{
			"model":                       model.ModelID,
			"prompt":                      prompt,
			"size":                        resolveArkImageSize(effectiveImageResolution(spec), spec.Aspect),
			"response_format":             "url",
			"watermark":                   false,
			"sequential_image_generation": "disabled",
			"seed":                        seed,
		}
		if len(referenceImages) == 1 {
			body["image"] = referenceImages[0]
		} else if len(referenceImages) > 1 {
			body["image"] = referenceImages
		}
	}
	raw, err := s.post(provider, path, body)
	if err != nil && isHTTPTimeout(err) {
		log.Printf("ark image gen timeout, retrying once: %v", err)
		raw, err = s.post(provider, path, body)
	}
	if err != nil {
		return "", err
	}
	url, err := parseImageGenerationURL(raw)
	if err != nil {
		return "", err
	}
	return url, nil
}

// GenerateImageEdit runs a single img2img-style edit with reference images
// (e.g. transition-frame mosaic annotation) and returns the generated image URL.
func (s *ArkService) GenerateImageEdit(provider models.AIProvider, model models.AIModel, prompt string, referenceImages []string, spec ImageGenSpec) (string, error) {
	return s.generateImageWithReferences(provider, model, prompt, referenceImages, 7, spec)
}

// generateXaisImage calls Gemini-compatible generateContent (no OpenAI /images/generations).
// Reference images are sent as inline_data base64 — no Tokyo relay / public URL required.
func (s *ArkService) generateXaisImage(provider models.AIProvider, model models.AIModel, prompt string, referenceImages []string, spec ImageGenSpec) (string, error) {
	started := time.Now()
	modelID := resolveXaisModelID(model.ModelID, effectiveImageResolution(spec))
	parts := make([]map[string]any, 0, len(referenceImages)+1)
	var refBytes int
	for i, ref := range referenceImages {
		part, n, err := s.xaisInlineImagePart(ref)
		if err != nil {
			return "", fmt.Errorf("参考图 #%d: %w", i+1, err)
		}
		refBytes += n
		parts = append(parts, part)
	}
	parts = append(parts, map[string]any{"text": prompt})
	prepMs := time.Since(started).Milliseconds()

	body := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": parts},
		},
	}
	if ratio := resolveXaisAspectRatio(modelID, spec.Aspect, effectiveImageResolution(spec)); ratio != "" {
		body["generationConfig"] = map[string]any{
			"imageConfig": map[string]any{
				"aspectRatio": ratio,
			},
		}
	}

	path := "/v1beta/models/" + url.PathEscape(modelID) + ":generateContent"
	log.Printf("xais generate start model=%s refs=%d refBytes≈%d prep=%dms", modelID, len(referenceImages), refBytes, prepMs)

	apiStart := time.Now()
	raw, err := s.post(provider, path, body)
	apiMs := time.Since(apiStart).Milliseconds()
	if err != nil {
		log.Printf("xais generate failed model=%s api=%dms total=%dms err=%v", modelID, apiMs, time.Since(started).Milliseconds(), err)
		return "", err
	}
	outURL, err := parseXaisGenerateContentURL(raw)
	log.Printf("xais generate ok model=%s api=%dms total=%dms", modelID, apiMs, time.Since(started).Milliseconds())
	return outURL, err
}

func resolveXaisModelID(modelID, resolution string) string {
	id := strings.TrimSpace(modelID)
	res := NormalizeImageResolution(resolution)

	// High-quality 4K SKU — fixed id from Xais price list.
	if id == "Xais Img2_4K_H" || id == "Xais_Img2_4K_H" || strings.EqualFold(id, "GPT Image HQ") {
		return "Xais Img2_4K_H"
	}

	switch {
	case id == "Image2" || strings.EqualFold(id, "GPT Image") || strings.EqualFold(id, "gpt-image") ||
		id == "Xais Img2" || id == "Xais_Img2" ||
		strings.HasPrefix(id, "Image2_") || strings.HasPrefix(id, "Xais Img2") || strings.HasPrefix(id, "Xais_Img2"):
		switch res {
		case "4k":
			return "Xais Img2_4K"
		case "2k":
			return "Xais Img2_2K"
		default:
			return "Xais Img2_1K"
		}
	case id == "Nano_Banana_2" || strings.HasPrefix(id, "Nano_Banana_2_"):
		switch res {
		case "4k":
			return "Nano_Banana_2_4K_0"
		case "2k":
			return "Nano_Banana_2_2K_0"
		default:
			// Banana 2 has no 1K SKU; Lite is the closest fast/1K option.
			return "Xais_Nano_Lite_1K"
		}
	case id == "Nano_Banana_Pro" || strings.HasPrefix(id, "Nano_Banana_Pro_"):
		if res == "4k" {
			return "Nano_Banana_Pro_4K_0"
		}
		return "Nano_Banana_Pro_2K_0"
	case id == "Xais_Nano_Lite_1K":
		return id
	}

	// Legacy full IDs with baked resolution: honor the resolution picker when possible.
	switch res {
	case "4k":
		id = strings.Replace(id, "_2K_", "_4K_", 1)
	case "2k", "1k":
		id = strings.Replace(id, "_4K_", "_2K_", 1)
	}
	return id
}

// resolveXaisAspectRatio returns Gemini imageConfig.aspectRatio.
// GPT Image (Img2) models expect pixel sizes; Banana models use ratio strings.
func resolveXaisAspectRatio(resolvedModelID, aspect, resolution string) string {
	a := NormalizeImageAspect(aspect, "")
	res := NormalizeImageResolution(resolution)
	isImg2 := strings.Contains(resolvedModelID, "Img2") || strings.HasPrefix(resolvedModelID, "Image2")
	if !isImg2 {
		return a
	}
	switch res {
	case "4k":
		switch a {
		case "9:16":
			return "2160x3840"
		case "16:9":
			return "3840x2160"
		case "2:1":
			return "3840x1920"
		default:
			return "2880x2880"
		}
	case "2k":
		switch a {
		case "9:16":
			return "1152x2048"
		case "16:9":
			return "2048x1152"
		case "2:1":
			return "2880x1440"
		default:
			return "2048x2048"
		}
	default:
		switch a {
		case "9:16":
			return "608x1088"
		case "16:9":
			return "1088x608"
		case "2:1":
			return "2048x1024"
		default:
			return "1024x1024"
		}
	}
}

func (s *ArkService) xaisInlineImagePart(ref string) (map[string]any, int, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, 0, errors.New("空参考图")
	}
	var raw []byte
	var ext string
	var err error
	if strings.HasPrefix(ref, "data:") || (!strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://")) {
		raw, ext, err = DecodeImageData(ref)
	} else {
		raw, err = s.DownloadImage(ref)
		if err != nil {
			return nil, 0, err
		}
		ext = "png"
		ct := http.DetectContentType(raw)
		switch {
		case strings.Contains(ct, "jpeg"):
			ext = "jpg"
		case strings.Contains(ct, "webp"):
			ext = "webp"
		case strings.Contains(ct, "png"):
			ext = "png"
		}
	}
	if err != nil {
		return nil, 0, err
	}
	// Shrink refs before Singapore upload — multi-MB originals make Xais img2img very slow.
	compressed, mime, cerr := compressImageForXais(raw, ext)
	if cerr == nil && len(compressed) > 0 {
		raw = compressed
	} else {
		mime = "image/png"
		switch strings.ToLower(ext) {
		case "jpg", "jpeg":
			mime = "image/jpeg"
		case "webp":
			mime = "image/webp"
		case "gif":
			mime = "image/gif"
		}
	}
	return map[string]any{
		"inline_data": map[string]any{
			"mime_type": mime,
			"data":      base64.StdEncoding.EncodeToString(raw),
		},
	}, len(raw), nil
}

// compressImageForXais downscales to maxSide and re-encodes as JPEG to cut upload+queue time.
func compressImageForXais(raw []byte, ext string) ([]byte, string, error) {
	const maxSide = 1280
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", errors.New("invalid image size")
	}
	scale := 1.0
	if w > maxSide || h > maxSide {
		if w >= h {
			scale = float64(maxSide) / float64(w)
		} else {
			scale = float64(maxSide) / float64(h)
		}
	}
	nw, nh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// Nearest-neighbor downsample — no extra deps; good enough for reference conditioning.
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, "", err
	}
	out := buf.Bytes()
	if len(out) >= len(raw) && scale >= 0.99 {
		// Already small; keep original.
		return raw, "", errors.New("no size gain")
	}
	log.Printf("xais ref compress %s %dx%d → %dx%d (%d → %d bytes)", ext, w, h, nw, nh, len(raw), len(out))
	return out, "image/jpeg", nil
}

var xaisMarkdownImageRE = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^)\s]+)\)`)

func parseXaisGenerateContentURL(raw []byte) (string, error) {
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text       string `json:"text"`
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
					InlineDataSnake *struct {
						MimeType string `json:"mime_type"`
						Data     string `json:"data"`
					} `json:"inline_data"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Error.Message != "" {
		return "", fmt.Errorf("%s", out.Error.Message)
	}
	if len(out.Candidates) == 0 {
		return "", errors.New("Xais 未返回图片")
	}
	for _, part := range out.Candidates[0].Content.Parts {
		if part.InlineData != nil && strings.TrimSpace(part.InlineData.Data) != "" {
			mime := firstNonEmpty(part.InlineData.MimeType, "image/png")
			return "data:" + mime + ";base64," + strings.TrimSpace(part.InlineData.Data), nil
		}
		if part.InlineDataSnake != nil && strings.TrimSpace(part.InlineDataSnake.Data) != "" {
			mime := firstNonEmpty(part.InlineDataSnake.MimeType, "image/png")
			return "data:" + mime + ";base64," + strings.TrimSpace(part.InlineDataSnake.Data), nil
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			if m := xaisMarkdownImageRE.FindStringSubmatch(text); len(m) > 1 {
				return m[1], nil
			}
			// bare URL fallback
			if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
				return strings.Fields(text)[0], nil
			}
		}
	}
	return "", errors.New("Xais 响应中未找到图片地址")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseImageGenerationURL(raw []byte) (string, error) {
	var out struct {
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Error.Message != "" {
		return "", fmt.Errorf("%s", out.Error.Message)
	}
	if len(out.Data) == 0 {
		return "", errors.New("模型未返回图片地址")
	}
	if u := strings.TrimSpace(out.Data[0].URL); u != "" {
		return u, nil
	}
	if b64 := strings.TrimSpace(out.Data[0].B64JSON); b64 != "" {
		// Persist path expects a fetchable URL or data URL.
		return "data:image/png;base64," + b64, nil
	}
	if rp := strings.TrimSpace(out.Data[0].RevisedPrompt); rp != "" {
		log.Printf("image gen returned revised_prompt but no image: %s", truncate(rp, 240))
	}
	return "", errors.New("模型未返回图片地址")
}

func buildPixAPIImageRequest(model models.AIModel, prompt string, referenceImages []string, spec ImageGenSpec) (string, map[string]any) {
	quality, size := resolvePixAPIImageParams(effectiveImageResolution(spec), spec.Aspect)
	body := map[string]any{
		"model":   model.ModelID,
		"prompt":  prompt,
		"n":       1,
		"size":    size,
		"quality": quality,
		// Prefer inline bytes so mainland hosts need not fetch r2.pixapi.ai (often times out).
		"response_format": "b64_json",
	}
	path := "/images/generations"
	if len(referenceImages) == 1 {
		body["image"] = referenceImages[0]
		path = "/images/edits"
	} else if len(referenceImages) > 1 {
		body["image"] = referenceImages
		path = "/images/edits"
	}
	return path, body
}

func validatePixAPIReferenceImages(referenceImages []string) error {
	for _, ref := range referenceImages {
		if strings.HasPrefix(ref, "data:") {
			return errors.New("PixAPI 图生图不支持 base64 参考图，请配置 PUBLIC_BASE_URL 使参考图可通过公网访问")
		}
		if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
			return errors.New("PixAPI 图生图要求参考图为 http(s) 公网地址")
		}
	}
	return nil
}

func (s *ArkService) DownloadImage(url string) ([]byte, error) {
	return s.DownloadImagePreferPix(url, shouldPreferPixDownload(url))
}

// DownloadImagePreferPix downloads an image. For PixAPI CDN URLs, prefers the Tokyo
// asset relay (PIXAPI_BASE_URL → http://tokyo:9080/r2/...) — same hop used for refs,
// inverted: Tokyo pulls r2 (fast), mainland pulls Tokyo (fast). Direct CDN is fallback only.
func (s *ArkService) DownloadImagePreferPix(rawURL string, preferPix bool) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "data:") {
		raw, _, err := DecodeImageData(rawURL)
		return raw, err
	}

	relayed := rewritePixAPIAssetURL(rawURL, s.PixAssetRelay)
	prefer := preferPix || shouldPreferPixDownload(rawURL)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		timeout := time.Duration(60+attempt*60) * time.Second // 120s, 180s, 240s

		// 1) Tokyo relay first — do NOT race with direct CDN from mainland (direct is usually hung).
		if relayed != "" && relayed != rawURL {
			if attempt == 1 {
				log.Printf("pixapi asset via tokyo relay: %s -> %s", truncateURL(rawURL), truncateURL(relayed))
			}
			// Relay is a nearby HTTP hop; use default client (not overseas proxy).
			data, err := s.downloadBytesOnce(context.Background(), relayed, timeout, false)
			if err == nil && len(data) > 0 {
				log.Printf("download image ok (%d bytes) via tokyo relay", len(data))
				return data, nil
			}
			lastErr = err
			log.Printf("tokyo asset relay failed attempt %d: %v", attempt, err)
		}

		// 2) Fallback: direct CDN (needs proxy / overseas egress).
		data, err := s.downloadBytesOnce(context.Background(), rawURL, timeout, prefer)
		if err == nil && len(data) > 0 {
			log.Printf("download image ok (%d bytes) via direct CDN", len(data))
			return data, nil
		}
		lastErr = err
		log.Printf("download image attempt %d/%d failed: %v", attempt, 3, err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("下载图片失败")
	}
	if s.PixAssetRelay == "" && isPixAPIAssetHostURL(rawURL) {
		return nil, fmt.Errorf("下载图片超时：未配置东京结果图中继（请设 PIXAPI_BASE_URL=http://东京IP:9080/v1 并确保中继已开 /r2/）。原始错误：%w", lastErr)
	}
	return nil, fmt.Errorf("下载图片超时或失败（已重试）：%w", lastErr)
}

func rewritePixAPIAssetURL(raw, assetRelayBase string) string {
	assetRelayBase = strings.TrimSuffix(strings.TrimSpace(assetRelayBase), "/")
	if assetRelayBase == "" {
		return raw
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	if !isPixAPIAssetHost(u.Hostname()) {
		return raw
	}
	out := assetRelayBase + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

func isPixAPIAssetHostURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isPixAPIAssetHost(u.Hostname())
}

func isPixAPIAssetHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "r2.pixapi.ai" || strings.HasSuffix(host, ".r2.cloudflarestorage.com") {
		return true
	}
	// Other PixAPI / OpenAI-style result CDNs occasionally seen in responses.
	for _, needle := range []string{
		"r2.pixapi", "pixapi.ai", "oaistatic.com", "openai.com",
		"blob.core.windows.net", "azureedge.net",
	} {
		if strings.Contains(host, needle) {
			return true
		}
	}
	return false
}

// DerivePixAPIRelayOrigin turns PIXAPI_BASE_URL=http://host:9080/v1 into http://host:9080.
func DerivePixAPIRelayOrigin(pixAPIBaseURL string) string {
	u, err := url.Parse(strings.TrimSpace(pixAPIBaseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "pixapi.ai") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// DerivePixAPIAssetRelay turns PIXAPI_BASE_URL into the result-image relay base.
//
//	http://host:9080/v1              → http://host:9080/r2
//	https://aiyixia.top/novaly-pixapi/v1 → https://aiyixia.top/novaly-pixapi/r2
func DerivePixAPIAssetRelay(pixAPIBaseURL, explicit string) string {
	if v := strings.TrimSuffix(strings.TrimSpace(explicit), "/"); v != "" {
		return v
	}
	u, err := url.Parse(strings.TrimSpace(pixAPIBaseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "pixapi.ai") {
		return ""
	}
	// Strip trailing /v1 (with or without slash).
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	path = strings.TrimSuffix(path, "/v1")
	if path == "" || path == "/" {
		return u.Scheme + "://" + u.Host + "/r2"
	}
	return u.Scheme + "://" + u.Host + path + "/r2"
}

// WrapPixAPIRefURL rewrites COS/app public URLs so PixAPI fetches via Tokyo relay:
//
//	https://bucket.cos.../projects/x/a.jpg  →  http://tokyo:9080/ref/cos/projects/x/a.jpg
//	http://app/api/uploads/projects/x/a.jpg →  http://tokyo:9080/ref/app/projects/x/a.jpg
func WrapPixAPIRefURL(publicURL, relayOrigin string, storage *Storage) string {
	relayOrigin = strings.TrimSuffix(strings.TrimSpace(relayOrigin), "/")
	if relayOrigin == "" || publicURL == "" {
		return publicURL
	}
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || u.Scheme == "" {
		return publicURL
	}
	path := u.EscapedPath()
	q := ""
	if u.RawQuery != "" {
		q = "?" + u.RawQuery
	}

	if storage != nil && storage.COSEnabled() {
		if key, ok := storage.COS.KeyFromPublicURL(publicURL); ok {
			return relayOrigin + "/ref/cos/" + strings.TrimPrefix(key, "/") + q
		}
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "myqcloud.com") || strings.Contains(host, "cos.") {
		return relayOrigin + "/ref/cos" + path + q
	}
	if i := strings.Index(path, "/api/uploads/"); i >= 0 {
		key := strings.TrimPrefix(path[i+len("/api/uploads/"):], "/")
		return relayOrigin + "/ref/app/" + key + q
	}
	return publicURL
}

// TruncateURLForLog is a small helper for controller logs.
func TruncateURLForLog(u string) string {
	return truncateURL(u)
}

func shouldPreferPixDownload(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, needle := range []string{
		"pixapi", "oaistatic", "openai", "azureedge", "blob.core.windows",
		"cloudflare", "amazonaws.com", "googleusercontent",
	} {
		if strings.Contains(host, needle) {
			return true
		}
	}
	return false
}

func (s *ArkService) createVideoTask(provider models.AIProvider, body map[string]any) (string, error) {
	raw, err := s.post(provider, "/contents/generations/tasks", body)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("未返回任务 ID")
	}
	return out.ID, nil
}

func (s *ArkService) waitVideoTask(provider models.AIProvider, taskID string, onETA func(string)) (string, error) {
	timeout := 3 * time.Minute
	if IsDoubaoWebAPI(provider) {
		// Doubao Seedance UI now often quotes ~15 minutes of queue; keep headroom.
		timeout = 25 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	var lastETA string
	for time.Now().Before(deadline) {
		raw, err := s.get(provider, "/contents/generations/tasks/"+taskID)
		if err != nil {
			return "", err
		}
		var out struct {
			Status  string `json:"status"`
			Error   any    `json:"error"`
			Content struct {
				VideoURL string `json:"video_url"`
			} `json:"content"`
			ETAText    string `json:"eta_text"`
			ETAMinutes int    `json:"eta_minutes"`
		}
		if err = json.Unmarshal(raw, &out); err != nil {
			return "", err
		}
		if onETA != nil {
			eta := strings.TrimSpace(out.ETAText)
			if eta == "" && out.ETAMinutes > 0 {
				eta = fmt.Sprintf("预计等待 %d 分钟", out.ETAMinutes)
			}
			if eta != "" && eta != lastETA {
				lastETA = eta
				onETA(eta)
			}
		}
		switch out.Status {
		case "succeeded":
			if out.Content.VideoURL == "" {
				return "", errors.New("任务成功但未返回视频地址")
			}
			return out.Content.VideoURL, nil
		case "failed", "expired", "cancelled":
			return "", formatVideoTaskError(out.Error)
		}
		time.Sleep(3 * time.Second)
	}
	return "", errors.New("视频生成超时（豆包排队常需 15 分钟以上），请稍后在任务中重试或到豆包页确认是否已生成")
}

func (s *ArkService) DownloadVideo(rawURL string) ([]byte, error) {
	candidates := videoDownloadCandidates(rawURL)
	if len(candidates) == 0 {
		return nil, errors.New("下载视频失败：无可用地址")
	}
	if len(candidates) == 1 {
		return s.downloadBytesOnce(context.Background(), candidates[0], 90*time.Second, false)
	}

	// Race proxy + CDN: whichever finishes first wins. Avoids waiting for a hung
	// localhost proxy (up to 120s) before trying the direct CDN URL.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	type result struct {
		data []byte
		err  error
		url  string
	}
	ch := make(chan result, len(candidates))
	for _, u := range candidates {
		go func(target string) {
			data, err := s.downloadBytesOnce(ctx, target, 0, false)
			ch <- result{data: data, err: err, url: target}
		}(u)
	}

	var lastErr error
	pending := len(candidates)
	for pending > 0 {
		r := <-ch
		pending--
		if r.err == nil && len(r.data) > 0 {
			cancel()
			log.Printf("download video ok (%d bytes) via %s", len(r.data), truncateURL(r.url))
			return r.data, nil
		}
		lastErr = r.err
		log.Printf("download video failed (%s): %v", truncateURL(r.url), r.err)
	}
	if lastErr == nil {
		lastErr = errors.New("下载视频失败：无可用地址")
	}
	return nil, lastErr
}

func videoDownloadCandidates(rawURL string) []string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	out := []string{rawURL}
	if unwrapped := unwrapDoubaoProxyURL(rawURL); unwrapped != "" && unwrapped != rawURL {
		out = append(out, unwrapped)
	}
	return out
}

func (s *ArkService) downloadVideoOnce(parent context.Context, target string, timeout time.Duration) ([]byte, error) {
	return s.downloadBytesOnce(parent, target, timeout, false)
}

func (s *ArkService) downloadBytesOnce(parent context.Context, target string, timeout time.Duration, preferPix bool) ([]byte, error) {
	ctx := parent
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	setCDNFetchHeaders(req, target)
	client := s.downloadClient(preferPix || shouldPreferPixDownload(target))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载视频失败 HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 80<<20))
}

func truncateURL(u string) string {
	if len(u) <= 120 {
		return u
	}
	return u[:117] + "..."
}

func unwrapDoubaoProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if strings.Contains(u.Path, "/images/proxy") {
		if orig := u.Query().Get("url"); orig != "" {
			return orig
		}
	}
	return ""
}

func setCDNFetchHeaders(req *http.Request, target string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	host := ""
	if u, err := url.Parse(target); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if strings.Contains(host, "douyin") || strings.Contains(host, "byte") || strings.Contains(host, "snssdk") || strings.Contains(host, "ibyte") {
		req.Header.Set("Referer", "https://www.doubao.com/chat/")
	}
}

func (s *ArkService) Test(provider models.AIProvider, model models.AIModel) error {
	if IsDoubaoWebAPI(provider) {
		return s.testDoubaoWeb(provider)
	}
	if IsPixAPI(provider) && model.Capability == "image" {
		_, err := s.generateImage(provider, model, "A minimal test icon on white background", 1, ImageGenSpec{Quality: "low", Aspect: "1:1"})
		return err
	}
	if IsXais(provider) && model.Capability == "image" {
		_, err := s.generateImage(provider, model, "A minimal test icon on white background", 1, ImageGenSpec{Quality: "low", Aspect: "1:1"})
		return err
	}
	_, err := s.chat(provider, map[string]any{"model": model.ModelID, "max_tokens": 8, "messages": []map[string]string{{"role": "user", "content": "回复 OK"}}})
	return err
}

func (s *ArkService) testDoubaoWeb(provider models.AIProvider) error {
	base := strings.TrimSuffix(provider.BaseURL, "/")
	base = strings.TrimSuffix(base, "/api/v3")
	req, err := http.NewRequest(http.MethodGet, base+"/health", nil)
	if err != nil {
		return err
	}
	s.setAuth(req, provider)
	resp, err := s.httpClient(provider).Do(req)
	if err != nil {
		return fmt.Errorf("无法连接豆包 Web API（请确认 doubao-web-api 已启动）：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("豆包 Web API 健康检查失败 HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

func (s *ArkService) setAuth(req *http.Request, provider models.AIProvider) {
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
}

// Chat is the exported text-completion entry used by crew agents and other packages.
func (s *ArkService) Chat(provider models.AIProvider, body map[string]any) (string, error) {
	return s.chat(provider, body)
}

func (s *ArkService) chat(provider models.AIProvider, body map[string]any) (string, error) {
	raw, err := s.post(provider, "/chat/completions", prepareChatBody(provider, body))
	if err != nil {
		return "", err
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("响应没有 choices")
	}
	content := strings.TrimSpace(decodeChatContent(decoded.Choices[0].Message.Content))
	if content == "" {
		return "", errors.New("响应内容为空")
	}
	return content, nil
}

// prepareChatBody copies the payload when a provider needs extra fields.
// DeepSeek V4 defaults to thinking mode; disable it so crew JSON stays in content.
func prepareChatBody(provider models.AIProvider, body map[string]any) map[string]any {
	if !IsDeepSeek(provider) {
		return body
	}
	out := make(map[string]any, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	if _, ok := out["thinking"]; !ok {
		out["thinking"] = map[string]any{"type": "disabled"}
	}
	return out
}

func decodeChatContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func logAPIRequest(provider models.AIProvider, path string, body map[string]any) {
	modelID, _ := body["model"].(string)

	if strings.Contains(path, "/chat/completions") {
		log.Printf("\n========== LLM PROMPT ==========\nprovider: %s\nmodel: %s\npath: %s\n--- messages ---\n%s\n================================\n",
			provider.Slug, modelID, path, formatChatMessages(body["messages"]))
		return
	}

	if strings.Contains(path, "/contents/generations/tasks") {
		text, images := summarizeVideoContent(body["content"])
		log.Printf("\n========== VIDEO REQUEST ==========\nprovider: %s\nmodel: %s\npath: %s\nratio: %v\nduration: %v\nimages: %d\n--- text ---\n%s\n===================================\n",
			provider.Slug, modelID, path, body["ratio"], body["duration"], images, text)
		return
	}

	if strings.Contains(path, ":generateContent") {
		text, refs := summarizeXaisContents(body["contents"])
		modelFromPath := modelID
		if modelFromPath == "" {
			// /v1beta/models/{id}:generateContent
			if i := strings.Index(path, "/models/"); i >= 0 {
				rest := path[i+len("/models/"):]
				if j := strings.Index(rest, ":"); j >= 0 {
					modelFromPath = rest[:j]
				}
			}
		}
		log.Printf("\n========== XAIS IMAGE ==========\nprovider: %s\nmodel: %s\npath: %s\nrefs: %d\n--- prompt ---\n%s\n================================\n",
			provider.Slug, modelFromPath, path, refs, text)
		return
	}

	if prompt, ok := body["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
		meta := []string{
			fmt.Sprintf("provider: %s", provider.Slug),
			fmt.Sprintf("model: %s", modelID),
			fmt.Sprintf("path: %s", path),
		}
		if seed, ok := body["seed"]; ok {
			meta = append(meta, fmt.Sprintf("seed: %v", seed))
		}
		if refs := countImageRefs(body["image"]); refs > 0 {
			meta = append(meta, fmt.Sprintf("refs: %d", refs))
		}
		log.Printf("\n========== IMAGE PROMPT ==========\n%s\n--- prompt ---\n%s\n==================================\n",
			strings.Join(meta, "\n"), prompt)
	}
}

func summarizeVideoContent(raw any) (text string, imageCount int) {
	arr, ok := raw.([]map[string]any)
	if !ok {
		b, err := json.Marshal(raw)
		if err != nil {
			return "(invalid content)", 0
		}
		var items []map[string]any
		if json.Unmarshal(b, &items) != nil {
			return string(b), 0
		}
		arr = items
	}
	var texts []string
	for _, item := range arr {
		switch item["type"] {
		case "text":
			if t, ok := item["text"].(string); ok && strings.TrimSpace(t) != "" {
				texts = append(texts, t)
			}
		case "image_url", "image":
			imageCount++
		}
	}
	if len(texts) == 0 {
		return "(empty text)", imageCount
	}
	return strings.Join(texts, "\n---\n"), imageCount
}

func summarizeXaisContents(raw any) (text string, imageCount int) {
	b, err := json.Marshal(raw)
	if err != nil {
		return "(invalid contents)", 0
	}
	var contents []struct {
		Parts []map[string]any `json:"parts"`
	}
	if json.Unmarshal(b, &contents) != nil {
		return "(unparsed contents)", 0
	}
	var texts []string
	for _, c := range contents {
		for _, part := range c.Parts {
			if t, ok := part["text"].(string); ok && strings.TrimSpace(t) != "" {
				texts = append(texts, t)
			}
			if _, ok := part["inline_data"]; ok {
				imageCount++
			}
			if _, ok := part["inlineData"]; ok {
				imageCount++
			}
		}
	}
	if len(texts) == 0 {
		return "(empty text)", imageCount
	}
	return strings.Join(texts, "\n---\n"), imageCount
}

func logAPIResponse(provider models.AIProvider, method, path string, status int, raw []byte) {
	body := string(raw)
	const maxLen = 16 << 10 // 16KB，避免 base64 图片把日志撑爆
	if len(body) > maxLen {
		body = body[:maxLen] + fmt.Sprintf("... (truncated, total %d bytes)", len(raw))
	}
	log.Printf("\n========== API RESPONSE ==========\nprovider: %s\n%s %s\nstatus: %d\n--- json ---\n%s\n==================================\n",
		provider.Slug, method, path, status, body)
}

func formatChatMessages(raw any) string {
	if raw == nil {
		return "(empty)"
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Sprintf("%v", raw)
	}
	var msgs []map[string]any
	if json.Unmarshal(b, &msgs) != nil {
		return string(b)
	}
	var lines []string
	for _, m := range msgs {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		lines = append(lines, fmt.Sprintf("[%s] %s", role, content))
	}
	if len(lines) == 0 {
		return string(b)
	}
	return strings.Join(lines, "\n")
}

func countImageRefs(raw any) int {
	if raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case []string:
		return len(v)
	case []any:
		return len(v)
	case string:
		if v != "" {
			return 1
		}
		return 0
	default:
		return 1
	}
}

func (s *ArkService) post(provider models.AIProvider, path string, body map[string]any) ([]byte, error) {
	logAPIRequest(provider, path, body)
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(provider.BaseURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	s.setAuth(req, provider)
	resp, err := s.httpClient(provider).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	logAPIResponse(provider, http.MethodPost, path, resp.StatusCode, raw)
	if resp.StatusCode >= 300 {
		return nil, parseArkError(resp.StatusCode, raw)
	}
	return raw, nil
}

func isHTTPTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "context deadline exceeded")
}

func formatVideoTaskError(err any) error {
	if err == nil {
		return errors.New("视频生成失败")
	}
	if raw, marshalErr := json.Marshal(err); marshalErr == nil {
		var parsed struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.Message != "" {
			return humanizeVideoError(fmt.Errorf("视频生成失败：%s", parsed.Message))
		}
	}
	return humanizeVideoError(fmt.Errorf("视频生成失败：%v", err))
}

func parseArkError(status int, raw []byte) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.Error.Message != "" {
		switch body.Error.Code {
		case "ModelNotOpen":
			return fmt.Errorf("模型未开通（%s），请到火山方舟控制台激活该模型", body.Error.Message)
		default:
			return fmt.Errorf("%s", body.Error.Message)
		}
	}
	msg := strings.TrimSpace(string(raw))
	if isXaisOverloadMessage(msg) {
		return fmt.Errorf("HTTP %d: %s", status, msg)
	}
	return fmt.Errorf("HTTP %d: %s", status, msg)
}

func isXaisGPTImageFamily(modelID string) bool {
	id := strings.TrimSpace(modelID)
	return id == "Image2" || strings.EqualFold(id, "GPT Image") || strings.EqualFold(id, "gpt-image") ||
		strings.EqualFold(id, "GPT Image HQ") ||
		strings.HasPrefix(id, "Image2_") || strings.HasPrefix(id, "Xais Img2") || strings.HasPrefix(id, "Xais_Img2")
}

func isXaisOverloadMessage(msg string) bool {
	u := strings.ToUpper(msg)
	return strings.Contains(u, "TASK_MODEL_OVERLOAD") || strings.Contains(u, "MODEL_OVERLOAD") || strings.Contains(u, "OVERLOADED")
}

func isXaisModelOverload(err error) bool {
	return err != nil && isXaisOverloadMessage(err.Error())
}

func humanizeXaisError(err error) error {
	if err == nil {
		return nil
	}
	if isXaisModelOverload(err) {
		return errors.New("Xais 模型当前过载繁忙，请稍后重试；或将分辨率改为 2K/4K，或改用 Nano Banana / Seedream")
	}
	return err
}

func (s *ArkService) get(provider models.AIProvider, path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(provider.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	s.setAuth(req, provider)
	resp, err := s.httpClient(provider).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	logAPIResponse(provider, http.MethodGet, path, resp.StatusCode, raw)
	if resp.StatusCode >= 300 {
		return nil, parseArkError(resp.StatusCode, raw)
	}
	return raw, nil
}
