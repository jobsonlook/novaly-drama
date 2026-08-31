package services

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const volcTTSV3Endpoint = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"

type VolcTTSConfig struct {
	AppID       string
	AccessToken string
	APIKey      string
	Cluster     string // unused for v3; kept for env compatibility
}

type VolcTTSService struct {
	cfg    VolcTTSConfig
	client *http.Client
}

func NewVolcTTSService(cfg VolcTTSConfig) *VolcTTSService {
	return &VolcTTSService{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (s *VolcTTSService) Configured() bool {
	if strings.TrimSpace(s.cfg.APIKey) != "" {
		return true
	}
	return strings.TrimSpace(s.cfg.AppID) != "" && strings.TrimSpace(s.cfg.AccessToken) != ""
}

type TTSSynthesizeInput struct {
	Text            string
	VoiceType       string
	SpeedRatio      float64 // legacy ratio; SpeechRate takes precedence
	SpeechRate      int     // official range: [-50, 100]
	Pitch           int     // additions.post_process.pitch, range: [-12, 12]
	LoudnessRate    int     // official range: [-50, 100]
	EnableEmotion   bool
	Emotion         string // supported API emotion enum, e.g. angry
	Tone            string // natural-language performance instruction
	EmotionStrength int    // dramatic intensity, range [1, 5]
	Encoding        string
}

type v3Request struct {
	User      v3User      `json:"user"`
	ReqParams v3ReqParams `json:"req_params"`
}

type v3User struct {
	UID string `json:"uid"`
}

type v3ReqParams struct {
	Text        string        `json:"text"`
	Speaker     string        `json:"speaker"`
	AudioParams v3AudioParams `json:"audio_params"`
	Additions   string        `json:"additions,omitempty"`
}

type v3AudioParams struct {
	Format       string `json:"format"`
	SampleRate   int    `json:"sample_rate"`
	SpeechRate   int    `json:"speech_rate,omitempty"`
	LoudnessRate int    `json:"loudness_rate,omitempty"`
	Emotion      string `json:"emotion,omitempty"`
}

type v3Chunk struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func resourceIDForVoice(voice string) string {
	if override := strings.TrimSpace(os.Getenv("VOLC_TTS_RESOURCE_ID")); override != "" {
		return override
	}
	v := strings.ToLower(strings.TrimSpace(voice))
	if strings.HasPrefix(v, "s_") {
		return "seed-icl-2.0"
	}
	// 官方 2.0 / ICL_uranus 系列
	if strings.Contains(v, "uranus") || strings.HasPrefix(v, "saturn_") {
		return "seed-tts-2.0"
	}
	if strings.HasPrefix(v, "icl_") {
		return "seed-tts-1.0"
	}
	return "seed-tts-1.0"
}

func speedRatioToSpeechRate(speed float64) int {
	if speed <= 0 {
		return 0
	}
	// speed_ratio 1.0 → 0; 0.5 → -50; 2.0 → 100
	rate := int((speed - 1.0) * 100)
	if rate < -50 {
		rate = -50
	}
	if rate > 100 {
		rate = 100
	}
	return rate
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// stripLeadingToneBrackets removes console-style tags wrongly embedded in text,
// e.g. "[决绝嘶吼]死，也要拉你们垫背！" → "死，也要拉你们垫背！"
func stripLeadingToneBrackets(text string) string {
	t := strings.TrimSpace(text)
	for {
		if strings.HasPrefix(t, "[") {
			end := strings.Index(t, "]")
			if end <= 0 {
				break
			}
			t = strings.TrimSpace(t[end+1:])
			continue
		}
		if strings.HasPrefix(t, "【") {
			end := strings.Index(t, "】")
			if end <= 0 {
				break
			}
			t = strings.TrimSpace(t[end+len("】"):])
			continue
		}
		break
	}
	return t
}

func containsAny(s string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}

// buildPerformanceInstruction turns a short tone tag into a concrete
// performance direction. SeedTTS responds more reliably to directions about
// intensity, breath, pacing, stress and emotional arc than to a bare emotion.
func buildPerformanceInstruction(tone string, strength int) string {
	t := strings.TrimSpace(tone)
	if t == "" {
		return ""
	}
	if strength <= 0 {
		strength = 4
	}
	strength = clamp(strength, 1, 5)
	// Earlier extraction prompts used dampening words that made SeedTTS read
	// dramatically charged lines almost neutrally. Keep the useful direction,
	// but remove those generic brakes before sending it as context_texts.
	t = strings.NewReplacer(
		"仅调整语气，不改变原本声线质感", "",
		"关键词适度重读", "关键词明显重读",
		"轻微颤抖", "明显颤抖",
		"轻微上扬", "明显上扬",
		"略微加快", "明显加快",
		"只略微加快", "明显加快",
	).Replace(t)
	t = strings.Trim(t, "；。 ")

	var scene, direction string
	switch {
	case containsAny(t, "嘶吼", "暴怒", "震怒", "疯魔", "咆哮"):
		scene = "像被逼入绝境后当面对仇人彻底爆发"
		direction = "胸腔强力发声，气息猛烈冲出；关键词狠狠咬住，语势快速推高，句尾必须炸开，允许自然破音和粗重呼吸"
	case containsAny(t, "决绝", "咬牙", "发力"):
		scene = "像已经做好付出生命代价的决定，面对最后一搏"
		direction = "先压住声音蓄力，再坚定推出；咬字有重量，关键词强重读，停顿紧绷，句尾斩钉截铁"
	case containsAny(t, "冰冷", "极冷", "冷酷", "冷傲", "杀机", "警告", "威严", "无情"):
		scene = "像掌握对方生死，近距离发出不可违抗的最后警告"
		direction = "声音低冷而有压迫感，语速放慢；每个关键词像刀锋一样清楚，停顿制造窒息感，句尾沉下去锁死"
	case containsAny(t, "虚弱", "喘息", "哀嚎", "奄奄", "无力"):
		scene = "像身受重伤倒在地上，疼痛中勉强挤出最后的话"
		direction = "气息断续发颤，字句被喘息切开；声音明显虚弱，偶有痛苦泄气，力量持续流失，句尾几乎撑不住"
	case containsAny(t, "惊恐", "惊慌", "错愕", "震惊", "秒怂", "颤抖", "破音"):
		scene = "像致命危险突然贴到眼前，本能地后退并失去镇定"
		direction = "开头明显倒吸气，声线绷紧发抖；语速忽停忽急，呼吸失序，关键处自然破音，恐惧要贯穿到句尾"
	case containsAny(t, "嘲讽", "阴阳怪气", "假笑", "娇笑", "嗤笑"):
		scene = "像居高临下看穿对方的窘态，故意当面羞辱"
		direction = "笑意和轻蔑必须听得出来；关键字故意拖长并错落重读，停顿像在欣赏对方难堪，句尾明显上挑"
	case containsAny(t, "生无可恋", "绝望", "悲伤", "委屈"):
		scene = "像刚失去最重要的人，强忍眼泪却已经快要崩溃"
		direction = "声音带明显哭腔和哽咽，呼吸沉重发抖；语速放慢，停顿里压着痛苦，句尾失去力量"
	case containsAny(t, "惊喜", "渴望", "兴奋", "激动"):
		scene = "像期待已久的愿望突然成真，兴奋得几乎控制不住"
		direction = "声音明亮向前，语速明显加快；关键词高扬重读，呼吸带笑，情绪持续升高，句尾保留强烈兴奋"
	case containsAny(t, "系统", "电子音"):
		scene = "像冷酷系统在危急时刻发布不可逆的判定"
		direction = "保持冷静、客观、近似系统播报的质感；音高和音量稳定，语速均匀，咬字精确；不加入多余情绪，句尾平稳收束"
	case containsAny(t, "画外音", "旁白", "字幕", "交代背景"):
		scene = "像电影预告片旁白，把观众直接带进当前危险氛围"
		direction = "声音沉稳但有戏剧张力；按画面变化安排停顿，核心信息明显重读，句尾留下悬念"
	default:
		scene = "把自己完全代入角色正在经历的真实冲突"
		direction = "不要播音腔和平读；用明显的轻重音、停顿、气息和音高变化推动情绪，句尾给出清晰态度"
	}

	return fmt.Sprintf(
		"请真实表演这句短剧台词，不要朗读。情绪强度%d/5：%s。%s。原始表演要求：%s。保持角色本音，但情绪必须鲜明可听，只说台词正文。",
		strength, scene, direction, t,
	)
}

func (s *VolcTTSService) Synthesize(in TTSSynthesizeInput) ([]byte, error) {
	if !s.Configured() {
		return nil, fmt.Errorf("未配置火山 TTS：请设置 VOLC_TTS_API_KEY，或 VOLC_TTS_APP_ID + VOLC_TTS_ACCESS_TOKEN")
	}
	text := strings.TrimSpace(in.Text)
	voice := strings.TrimSpace(in.VoiceType)
	if text == "" {
		return nil, fmt.Errorf("text 不能为空")
	}
	if voice == "" {
		return nil, fmt.Errorf("voice_type 不能为空")
	}
	encoding := strings.TrimSpace(in.Encoding)
	if encoding == "" {
		encoding = "mp3"
	}

	additionsObj := map[string]any{}
	tone := strings.TrimSpace(in.Tone)
	resourceID := resourceIDForVoice(voice)
	// 正文只合成台词。中括号标签写进 text 会被读出来。
	// 语气走语音指令 context_texts（官方文档推荐写法）。
	synthText := stripLeadingToneBrackets(text)
	if in.EnableEmotion && tone != "" {
		additionsObj["context_texts"] = []string{
			buildPerformanceInstruction(tone, in.EmotionStrength),
		}
		if resourceID == "seed-tts-2.0" {
			additionsObj["model"] = "seed-tts-2.0-expressive"
		}
	}
	if in.Pitch != 0 {
		additionsObj["post_process"] = map[string]int{
			"pitch": clamp(in.Pitch, -12, 12),
		}
	}
	if strings.HasPrefix(strings.ToLower(voice), "s_") {
		additionsObj["model_type"] = 4
	}
	additions := ""
	if len(additionsObj) > 0 {
		b, err := json.Marshal(additionsObj)
		if err != nil {
			return nil, err
		}
		additions = string(b)
	}

	speechRate := in.SpeechRate
	if speechRate == 0 {
		speechRate = speedRatioToSpeechRate(in.SpeedRatio)
	}
	emotion := ""
	// SeedTTS 2.0 uses context_texts for natural-language emotion control.
	// Sending audio_params.emotion at the same time stacks another global
	// transformation and can noticeably shift the base voice on short lines.
	if in.EnableEmotion && resourceID == "seed-tts-1.0" {
		emotion = strings.TrimSpace(in.Emotion)
	}
	payload := v3Request{
		User: v3User{UID: "novaly"},
		ReqParams: v3ReqParams{
			Text:    synthText,
			Speaker: voice,
			AudioParams: v3AudioParams{
				Format:       encoding,
				SampleRate:   24000,
				SpeechRate:   clamp(speechRate, -50, 100),
				LoudnessRate: clamp(in.LoudnessRate, -50, 100),
				Emotion:      emotion,
			},
			Additions: additions,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, volcTTSV3Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Resource-Id", resourceID)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
	if key := strings.TrimSpace(s.cfg.APIKey); key != "" {
		req.Header.Set("X-Api-Key", key)
	} else {
		req.Header.Set("X-Api-App-Id", strings.TrimSpace(s.cfg.AppID))
		req.Header.Set("X-Api-Access-Key", strings.TrimSpace(s.cfg.AccessToken))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用火山 TTS 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s", formatTTSHTTPError(resp.StatusCode, raw, resourceID, voice))
	}

	audio, err := decodeV3Audio(raw)
	if err != nil {
		return nil, err
	}
	return audio, nil
}

func formatTTSHTTPError(status int, raw []byte, resourceID, voice string) string {
	msg := truncate(string(raw), 400)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not granted") || strings.Contains(msg, "45000030") {
		return fmt.Sprintf(
			"火山 TTS 资源未开通（HTTP %d，resource=%s，voice=%s）。请到火山控制台开通「语音合成大模型」：https://console.volcengine.com/speech/service → 开通/创建实例（SeedTTS 2.0）后重试。原始错误：%s",
			status, resourceID, voice, msg,
		)
	}
	if strings.Contains(msg, "45000010") || strings.Contains(lower, "invalid x-api-key") {
		return fmt.Sprintf("火山 TTS API Key 无效（HTTP %d）。请确认 .env 的 VOLC_TTS_API_KEY 填的是控制台「API Key」列的 UUID，不是名称。原始错误：%s", status, msg)
	}
	return fmt.Sprintf("火山 TTS HTTP %d (resource=%s): %s", status, resourceID, msg)
}

func decodeV3Audio(raw []byte) ([]byte, error) {
	var chunks [][]byte
	var lastErr string
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	// audio chunks can be large
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk v3Chunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			// some responses are a single JSON object
			continue
		}
		if chunk.Code == 0 && chunk.Data != "" {
			part, err := base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				return nil, fmt.Errorf("解码音频失败: %w", err)
			}
			chunks = append(chunks, part)
			continue
		}
		if chunk.Code == 20000000 {
			break
		}
		if chunk.Code != 0 {
			msg := chunk.Message
			if msg == "" {
				msg = line
			}
			lastErr = fmt.Sprintf("code=%d: %s", chunk.Code, truncate(msg, 200))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		// fallback: whole body as one JSON
		var single v3Chunk
		if err := json.Unmarshal(raw, &single); err == nil {
			if single.Code != 0 && single.Code != 20000000 {
				return nil, fmt.Errorf("火山 TTS 错误 code=%d: %s", single.Code, single.Message)
			}
			if single.Data != "" {
				part, err := base64.StdEncoding.DecodeString(single.Data)
				if err != nil {
					return nil, fmt.Errorf("解码音频失败: %w", err)
				}
				return part, nil
			}
		}
		if lastErr != "" {
			return nil, fmt.Errorf("火山 TTS 错误 %s", lastErr)
		}
		return nil, fmt.Errorf("火山 TTS 未返回音频数据: %s", truncate(string(raw), 300))
	}
	return bytes.Join(chunks, nil), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
