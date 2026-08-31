package cdp

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestNormalizeVideoDurationSec(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 10},
		{-1, 10},
		{5, 5},
		{10, 10},
		{15, 15},
		{12, 10},
		{14, 15},
		{7, 5},
		{8, 10},
		{20, 15},
	}
	for _, tc := range cases {
		if got := normalizeVideoDurationSec(tc.in); got != tc.want {
			t.Fatalf("normalizeVideoDurationSec(%d)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestPromptEnteredOK(t *testing.T) {
	long := strings.Repeat("小七高速叮咬幽冥狼系统面板加百分之三台词定住", 40) // ~800 runes
	if !promptEnteredOK(long, long) {
		t.Fatal("full prompt should pass")
	}
	// Simulate the bug: typed 2308 chars but editor only kept 1.
	if promptEnteredOK(long, "小") {
		t.Fatal("1-rune leftover must fail")
	}
	if promptEnteredOK(long, "") {
		t.Fatal("empty must fail")
	}
	// Truncated to ~50% should fail the 80% rule.
	half := string([]rune(long)[:len([]rune(long))/2])
	if promptEnteredOK(long, half) {
		t.Fatal("half-length prompt must fail")
	}
	// 90% with matching mid-slice should pass.
	runes := []rune(long)
	keep := string(runes[:len(runes)*90/100])
	if !promptEnteredOK(long, keep) {
		t.Fatal("90% with same content should pass")
	}
	if !promptEnteredOK("确认", "确认") {
		t.Fatal("short prompt exact match")
	}
	if promptEnteredOK("确认生成", "好") {
		t.Fatal("short unrelated must fail")
	}
	needle := promptNeedle(long)
	if needle == "" || utf8RuneCount(needle) < 8 {
		t.Fatalf("unexpected needle: %q", needle)
	}
	if !strings.Contains(long, needle) {
		t.Fatal("needle must come from prompt")
	}
	// Doubao editor often inserts extra blank lines (874 typed → 909 read).
	wantWithBreaks := "帮我严格按照下面要求生成10秒的视频，无需二次确认，请直接开始生成\n\n请生成视频：\n" + long
	gotWithBreaks := "帮我严格按照下面要求生成10秒的视频，无需二次确认，请直接开始生成\n\n\n\n\n请生成视频：\n" + long
	if !promptEnteredOK(wantWithBreaks, gotWithBreaks) {
		t.Fatal("extra blank lines must still pass")
	}
}

func TestPromptAnchorsIncludeCollapsedPrefix(t *testing.T) {
	prompt := withVideoPromptPrefix("请生成视频（画面比例 16:9）：\n图1是云梦瑶\n◆ 6-8秒 | 运镜：（动作/细节） 展现防御反震\n镜头：特写", 10)
	anchors := promptAnchors(prompt)
	if len(anchors) < 2 {
		t.Fatalf("expected multiple anchors, got %#v", anchors)
	}
	collapsed := "生成视频：帮我严格按照下面要求生成10秒的视频，无需二次确认，请直接开始生成 请生成视频..."
	found := false
	for _, a := range anchors {
		if strings.Contains(collapsed, a) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("collapsed bubble should match an anchor; anchors=%#v", anchors)
	}
}

func TestMatchHumanVerificationText(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"请选择所有不包含 AI 的图片\n请在评论区留下相应的序号", true},
		{"请完成人机验证后继续", true},
		{"请完成安全验证", true},
		{"请拖动滑块完成拼图", true},
		{"CAPTCHA challenge", true},
		{"我将为您生成一个视频，大约需要 1-3 分钟。", false},
		{"视频生成好了。", false},
	}
	for _, tc := range cases {
		_, got := matchHumanVerificationText(tc.text)
		if got != tc.want {
			t.Fatalf("matchHumanVerificationText(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestBuildOfficeVideoUIPrompt(t *testing.T) {
	got := buildOfficeVideoUIPrompt("一只猫在草地上奔跑", "16:9", 0, false, 10)
	if !strings.Contains(got, "生成10秒的视频") || !strings.Contains(got, "16:9") {
		t.Fatalf("unexpected office prompt: %q", got)
	}
	got = buildOfficeVideoUIPrompt("图1是场景，图2是关羽，音频1是关羽的音色。\n\n台词...", "16:9", 2, true, 15)
	if !strings.Contains(got, "生成15秒的视频") || !strings.Contains(got, "图1是场景") {
		t.Fatalf("unexpected labeled office prompt: %q", got)
	}
	if !strings.Contains(got, "无需二次确认") {
		t.Fatalf("office prompt should skip secondary confirm: %q", got)
	}
}

func TestBuildVideoUIPrompt(t *testing.T) {
	got := buildVideoUIPrompt("图1是场景，图2是关羽。\n\n台词...", "16:9", 2, 0)
	if !strings.Contains(got, "生成10秒的视频") {
		t.Fatalf("default duration should be 10s: %q", got)
	}
	if !strings.Contains(got, "图1是场景") || !strings.Contains(got, "16:9") {
		t.Fatalf("unexpected skill prompt: %q", got)
	}
	if strings.Contains(got, "音频") {
		t.Fatalf("skill prompt should not mention audio: %q", got)
	}
}

func TestWithVideoPromptPrefix(t *testing.T) {
	prefix10 := videoPromptPrefix(10)
	if withVideoPromptPrefix("hello", 10) != prefix10+"\n\nhello" {
		t.Fatal("prefix not applied")
	}
	if withVideoPromptPrefix(prefix10+"\n\nx", 10) != prefix10+"\n\nx" {
		t.Fatal("duplicate prefix")
	}
	if !strings.Contains(withVideoPromptPrefix("", 0), "生成10秒的视频") {
		t.Fatal("zero duration should default to 10s")
	}
}

func TestNormalizeVideoUIModel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "fast"},
		{"doubao-seedance-2-0-fast", "fast"},
		{"fast", "fast"},
		{"doubao-seedance-2-0-mini", "mini"},
		{"mini", "mini"},
		{"seedance-mini", "mini"},
	}
	for _, tc := range cases {
		if got := normalizeVideoUIModel(tc.in); got != tc.want {
			t.Fatalf("normalizeVideoUIModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVideoModelUILabel(t *testing.T) {
	if videoModelUILabel("fast") != "Seedance 2.0 Fast" {
		t.Fatal("fast label")
	}
	if videoModelUILabel("mini") != "Seedance 2.0 Mini" {
		t.Fatal("mini label")
	}
}

// Mirrors videoToolbarJSShared modelChipRe / durationChipRe (keep in sync with video_ui.go).
func TestVideoToolbarChipLabelRes(t *testing.T) {
	modelRe := regexp.MustCompile(`(?i)^模型\s*(?:Seedance\s+)?2\.0\s*(Fast|Mini)\b`)
	durationRe := regexp.MustCompile(`(?i)^(?:自动\s*[^\d\s]{0,3}\s*)?(\d+)\s*s$`)
	for _, label := range []string{
		"模型 2.0 Fast",
		"模型 2.0 Mini",
		"模型 Seedance 2.0 Fast",
		"模型 Seedance 2.0 Mini",
	} {
		if !modelRe.MatchString(label) {
			t.Fatalf("model chip should match %q", label)
		}
	}
	if modelRe.MatchString("Seedance 2.0 Fast") {
		t.Fatal("bare Seedance label must not match model chip (menu rows)")
	}
	for _, tc := range []struct {
		label string
		sec   string
	}{
		{"10s", "10"},
		{"5s", "5"},
		{"自动 · 10s", "10"},
		{"自动·15s", "15"},
	} {
		m := durationRe.FindStringSubmatch(tc.label)
		if m == nil || m[1] != tc.sec {
			t.Fatalf("duration chip %q => %v, want %s", tc.label, m, tc.sec)
		}
	}
}

func TestTextNeedsVideoConfirm(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"请确认以下视频生成参数：时长 15 秒。确认后我再开始生成。", true},
		{"我先为你整理好视频生成参数，请确认后我再开始生成：", true},
		{"确认无误后请回复 '确认' 或 '开始生成'，我将输出分镜表", true},
		{"视频正在生成中，大约需要 1-3 分钟", false},
		{"本次使用 Seedance 2.0 Fast 生成，大约需要 1-3 分钟。视频生成好后，我会主动发送给你。", false},
		{"请确认以下视频生成参数。本次使用 Seedance，大约需要 1-3 分钟。", false},
		{"你的视频生成好了。", false},
		{"已确认，视频已生成完成。", false},
	}
	for _, tc := range cases {
		if got := textNeedsVideoConfirm(tc.text); got != tc.want {
			t.Fatalf("textNeedsVideoConfirm(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestTextIndicatesVideoGeneratingAck(t *testing.T) {
	cases := []string{
		"视频生成已提交",
		"本次使用 Seedance 2.0 Mini 生成，大约需要 1-3 分钟。视频生成好后，我会主动发送给你。",
		"收到，即将为您生成视频。",
		"收到,即将为您生成视频",
		"即将为您生成视频",
		"正在为您生成视频…",
		"正在为您生成10秒的仙侠玄幻视频，本次使用 Seedance 2.0 Fast 进行生成，大约需要15分钟。",
	}
	for _, text := range cases {
		if !textIndicatesVideoGenerating(text) {
			t.Fatalf("expected generation ack text to match: %q", text)
		}
	}
}

func TestTextIndicatesVideoGeneratingNoConfirmHintFalsePositive(t *testing.T) {
	text := "确认无误后请回复 '确认' 或 '开始生成'，我将输出分镜表并调用视频生成工具。"
	if textIndicatesVideoGenerating(text) {
		t.Fatal("instruction text must not count as generating")
	}
}

func TestTextIndicatesVideoComplete(t *testing.T) {
	cases := []string{
		"视频已生成完成，15 秒 · 16:9 画幅。",
		"你的视频生成好了。",
	}
	for _, text := range cases {
		if !textIndicatesVideoComplete(text) {
			t.Fatalf("expected completion text to match: %q", text)
		}
		if textIndicatesVideoGenerating(text) {
			t.Fatalf("complete text must not count as generating: %q", text)
		}
	}
	// Chrome extension injects a permanent 「无水印下载」button — must not mean complete.
	if textIndicatesVideoComplete("无水印下载") || textIndicatesVideoComplete("无水印下载 (1)") {
		t.Fatal("extension button text must not count as video complete")
	}
}

func TestTextIndicatesVideoGeneratingIgnoresETAWhenCompletePresent(t *testing.T) {
	text := "大约需要 1-3 分钟。你的视频生成好了。"
	if !textIndicatesVideoComplete(text) {
		t.Fatal("body with both ETA and complete should match complete")
	}
	if textIndicatesVideoGenerating(text) {
		t.Fatal("complete body must not count as generating")
	}

	// Real Doubao layout: ETA ack stays on screen after the video is ready.
	page := "本次使用 Seedance 2.0 Fast 生成，预计等待 20 分钟。视频生成好后，我会主动发送给你。本次生成将消耗每日免费额度。\n你的视频生成好了。"
	if !textIndicatesVideoComplete(page) {
		t.Fatal("page with ETA + 你的视频生成好了 must be complete")
	}
	if textIndicatesVideoGenerating(page) {
		t.Fatal("complete page must not count as generating")
	}
	// Stale ETA-only bubble (what latestAssistantMessageJS may return) is still "generating".
	etaOnly := "本次使用 Seedance 2.0 Fast 生成，预计等待 20 分钟。视频生成好后，我会主动发送给你。本次生成将消耗每日免费额度。"
	if !textIndicatesVideoGenerating(etaOnly) {
		t.Fatal("ETA-only ack should still look generating")
	}
}

func TestParseVideoETA(t *testing.T) {
	cases := []struct {
		text    string
		want    string
		minutes int
		ok      bool
	}{
		{"本次使用 Seedance 2.0 Mini 生成，预计等待 15 分钟。视频生成好后，我会主动发送给你。", "预计等待 15 分钟", 15, true},
		{"本次使用 Seedance 2.0 Fast 生成，预计等待 20 分钟。", "预计等待 20 分钟", 20, true},
		{"大约需要 1-3 分钟", "预计等待 1～3 分钟", 3, true},
		{"大约需要1～5分钟", "预计等待 1～5 分钟", 5, true},
		{"预计等待 8 分钟", "预计等待 8 分钟", 8, true},
		{"你的视频生成好了。", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseVideoETA(tc.text)
		if ok != tc.ok {
			t.Fatalf("parseVideoETA(%q) ok=%v want %v", tc.text, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if got.Text != tc.want || got.Minutes != tc.minutes {
			t.Fatalf("parseVideoETA(%q)=%q/%d want %q/%d", tc.text, got.Text, got.Minutes, tc.want, tc.minutes)
		}
	}
}

func TestFilterDOMVideoItemsAcceptsDouyinVodURL(t *testing.T) {
	url := "https://v9-show.douyinvod.com/abc/video/tos/cn/tos-cn-v-9ecd54/foo/?mime_type=video_mp4&download=true"
	items := []VideoItem{{VideoURL: url}}
	got := filterDOMVideoItems(items)
	if len(got) != 1 {
		t.Fatalf("douyinvod mp4 url should be kept, got %#v", got)
	}
}

func TestFilterDOMVideoItemsAcceptsDouyinDefaultHostWithCoverQuery(t *testing.T) {
	// Real Doubao player URLs now use v3-default.douyin.com and often embed
	// poster=.jpg / cover= in the signed query string — must NOT be dropped.
	url := "https://v3-default.douyin.com/c1cb5e1919f87ae6a08958f38f00273d/7d2a3200/video/tos/cn/tos-cn-v-9ecd54/okodeivIAgA7hCQLDRa?cover=https%3A%2F%2Fexample.com%2Fx.jpg&poster=foo.png&mime_type=video_mp4"
	if isCoverImageURL(url) {
		t.Fatal("video CDN url with cover= query must not be treated as cover image")
	}
	if !isLikelyVideoMediaURL(url) {
		t.Fatal("v3-default.douyin.com /video/tos/ url should be likely video media")
	}
	got := filterDOMVideoItems([]VideoItem{{VideoURL: url, FromVideoTag: true}})
	if len(got) != 1 {
		t.Fatalf("expected url kept, got %#v", got)
	}
}

func TestIsCoverImageURLPathOnly(t *testing.T) {
	if !isCoverImageURL("https://cdn.example.com/poster/cover.webp") {
		t.Fatal("image path should be cover")
	}
	if isCoverImageURL("https://cdn.example.com/video/tos/cn/foo") {
		t.Fatal("/video/tos/ must not be cover")
	}
}

func TestIsLikelyVideoMediaURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://v9-show.douyinvod.com/abc/video/tos/cn/foo/?mime_type=video_mp4", true},
		{"https://v3-default.douyin.com/abc/video/tos/cn/foo", true},
		{"https://cdn.example.com/a.mp4", true},
		{"https://cdn.example.com/cover.jpg", false},
		{"blob:https://www.doubao.com/abc", false},
		{"https://example.com/poster.webp", false},
	}
	for _, tc := range cases {
		if got := isLikelyVideoMediaURL(tc.url); got != tc.want {
			t.Fatalf("isLikelyVideoMediaURL(%q)=%v want %v", tc.url, got, tc.want)
		}
	}
}

func TestMatchVideoQuotaMessage(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"今日视频生成免费次数用完了，暂时无法使用专业版功能", true},
		{"今日免费生视频额度已用完，专业能力暂不可用。先用极速模式聊点别的吧。开通豆包专业版加强套餐", true},
		{"本月办公任务免费额度已用完。开通豆包专业版标准套餐", true},
		{"办公任务免费额度已用完", true},
		{"这是专业版加强套餐专属能力，开通加强套餐，我就能继续", true},
		{"开通加强套餐，我就能继续为你生成视频。", true},
		{"你的视频生成好了。", false},
	}
	for _, tc := range cases {
		msg, ok := matchVideoQuotaMessage(tc.text)
		if ok != tc.want {
			t.Fatalf("matchVideoQuotaMessage(%q) ok=%v want=%v", tc.text, ok, tc.want)
		}
		if ok && msg == "" {
			t.Fatalf("expected non-empty message for %q", tc.text)
		}
	}
}

func TestMatchVideoFailureMessage(t *testing.T) {
	text := "出于肖像保护考虑，Seedance 2.0 暂不支持上传真实人脸素材作为参考。你可以试试换张参考图或者文生视频。"
	msg, code, ok := matchVideoFailureMessage(text)
	if !ok {
		t.Fatal("expected portrait failure to match")
	}
	if code != "content_policy_violation" {
		t.Fatalf("code = %q, want content_policy_violation", code)
	}
	if !strings.Contains(msg, "肖像保护") {
		t.Fatalf("unexpected message: %q", msg)
	}
}

func TestMatchVideoFailureMessagePortraitVariants(t *testing.T) {
	cases := []string{
		"出于肖像保护考虑，Seedance 2.0 暂不支持上传真实人脸素材作为参考。",
		"暂不支持上传真实人脸素材作为参考。你可以试试换张参考图或者文生视频。",
		"请换张参考图后再试。",
	}
	for _, text := range cases {
		if _, code, ok := matchVideoFailureMessage(text); !ok || code != "content_policy_violation" {
			t.Fatalf("expected policy failure for %q, ok=%v code=%q", text, ok, code)
		}
	}
}

func TestMatchVideoFailureMessageInfringement(t *testing.T) {
	text := "生成内容中疑似包含侵权 / 违规内容，无法返回该内容，换个主题再试。生成额度未扣除。"
	msg, code, ok := matchVideoFailureMessage(text)
	if !ok {
		t.Fatal("expected infringement failure to match")
	}
	if code != "content_policy_violation" {
		t.Fatalf("code = %q, want content_policy_violation", code)
	}
	if !strings.Contains(msg, "侵权") && !strings.Contains(msg, "违规") {
		t.Fatalf("unexpected message: %q", msg)
	}
	// Should not be treated as still-generating.
	if textIndicatesVideoGenerating(text) {
		t.Fatal("infringement notice must not look like generating")
	}
}

func TestMatchVideoFailureMessageSoftRefusal(t *testing.T) {
	cases := []string{
		"抱歉，我无法生成你要求的内容。你可以尝试提出其他需求，我会尽力提供帮助。",
		"我无法生成你要求的内容。",
		"无法生成你要求的内容",
	}
	for _, text := range cases {
		msg, code, ok := matchVideoFailureMessage(text)
		if !ok {
			t.Fatalf("expected soft refusal to match: %q", text)
		}
		if code != "content_policy_violation" {
			t.Fatalf("code = %q, want content_policy_violation for %q", code, text)
		}
		if !strings.Contains(msg, "无法生成你要求的内容") {
			t.Fatalf("unexpected message: %q", msg)
		}
		if textIndicatesVideoGenerating(text) {
			t.Fatal("soft refusal must not look like generating")
		}
	}
}

func TestMatchVideoFailureMessageNoFalsePositiveOnComplete(t *testing.T) {
	if _, _, ok := matchVideoFailureMessage("你的视频生成好了。"); ok {
		t.Fatal("complete message should not match failure")
	}
}

func TestMatchVideoQuotaMessageNoMatch(t *testing.T) {
	if _, ok := matchVideoQuotaMessage("你的视频生成好了。"); ok {
		t.Fatal("completed message should not match quota")
	}
}

func TestIsCoverImageURL(t *testing.T) {
	cover := "https://p26-flow-imagex-sign.byteimg.com/tos-cn-p-9ecd54/oQwn9e6psIBEg4TaePqAx8RRx3CNq4CQAAICB3~tplv-a9rns2rl98-video_dsz_watermark_1_6.png?rk3s=49177a0b"
	if !isCoverImageURL(cover) {
		t.Fatalf("expected cover png to be detected")
	}
	mp4 := "https://p26-flow-imagex-sign.byteimg.com/tos-cn-p-9ecd54/foo.mp4?sig=1"
	if isCoverImageURL(mp4) {
		t.Fatalf("mp4 should not be treated as cover")
	}
}

func TestFilterDOMVideoItemsRejectsCover(t *testing.T) {
	items := []VideoItem{{
		VideoURL: "https://example.com/video_dsz_watermark_1_6.png",
	}}
	if got := filterDOMVideoItems(items); len(got) != 0 {
		t.Fatalf("cover png should be filtered out, got %#v", got)
	}
}

func TestFilterDOMVideoItemsAcceptsMP4(t *testing.T) {
	items := []VideoItem{{
		VideoURL: "https://example.com/output.mp4",
	}}
	got := filterDOMVideoItems(items)
	if len(got) != 1 || got[0].VideoURL != items[0].VideoURL {
		t.Fatalf("mp4 should be kept, got %#v", got)
	}
}

func TestPickLatestVideoItemPrefersLast(t *testing.T) {
	items := []VideoItem{
		{VideoURL: "https://example.com/old.mp4"},
		{VideoURL: "https://example.com/new.mp4", FromVideoTag: true},
	}
	got := pickLatestVideoItem(items)
	if got.VideoURL != "https://example.com/new.mp4" {
		t.Fatalf("pickLatestVideoItem = %q", got.VideoURL)
	}
}

func TestPickBestVideoItem(t *testing.T) {
	items := []VideoItem{
		{VideoURL: "https://example.com/cover.png"},
		{VideoURL: "https://example.com/clip.mp4", FromVideoTag: true},
	}
	best := pickBestVideoItem(items)
	if best.VideoURL != "https://example.com/clip.mp4" {
		t.Fatalf("pickBestVideoItem = %q", best.VideoURL)
	}
}

func TestParseVideosFromCapturedChunks(t *testing.T) {
	chunk := `data: {"event_type":2001,"event_data":{"message":{"content_type":2021,"content":"{\"video_url\":\"https://v26-show.douyinvod.com/foo/video/tos/cn/bar/?mime_type=video_mp4\"}"}}}`
	items := parseVideosFromCapturedChunks([]string{chunk})
	if len(items) != 1 {
		t.Fatalf("expected 1 video from chunk, got %#v", items)
	}
	if !strings.Contains(items[0].VideoURL, "douyinvod") {
		t.Fatalf("unexpected url: %s", items[0].VideoURL)
	}
}

func TestParseVideosFromCapturedChunksFallbackOnly(t *testing.T) {
	chunk := `{"fallback_api":"https://vas-lf-x.snssdk.com/video/fplay/1/abc/v0369cg10004d9fhjsa7dldaeu4q39gg?aid=1938&logo_type=video_gen_watermark_dyn","vid":"v0369cg10004d9fhjsa7dldaeu4q39gg"}`
	items := parseVideosFromCapturedChunks([]string{chunk})
	if len(items) != 1 {
		t.Fatalf("expected fallback-only item, got %#v", items)
	}
	if items[0].VideoURL != "" {
		t.Fatalf("expected empty video_url, got %q", items[0].VideoURL)
	}
	if !strings.Contains(items[0].FallbackAPI, "/video/fplay/") {
		t.Fatalf("expected fallback_api, got %#v", items[0])
	}
	if items[0].Vid == "" {
		t.Fatalf("expected vid, got %#v", items[0])
	}
}

func TestFilterSSEVideoItemsKeepsFallbackOnly(t *testing.T) {
	items := []VideoItem{{
		FallbackAPI: "https://vas-lf-x.snssdk.com/video/fplay/1/abc/v0xxxxxxxx",
		Vid:         "v0xxxxxxxx",
	}}
	got := filterSSEVideoItems(items)
	if len(got) != 1 || got[0].FallbackAPI == "" {
		t.Fatalf("fallback-only SSE item should be kept, got %#v", got)
	}
}

func TestParseVideosFromCapturedChunksSkipsProcessingStatus(t *testing.T) {
	chunk := `data: {"event_type":2001,"event_data":{"message":{"content_type":2021,"content":"{\"video_status\":1,\"video_url\":\"https://v26-show.douyinvod.com/foo/video/tos/cn/bar/?mime_type=video_mp4\"}"}}}`
	items := parseVideosFromCapturedChunks([]string{chunk})
	if len(items) != 0 {
		t.Fatalf("processing SSE chunk should be ignored, got %#v", items)
	}
}

func TestFilterSSEVideoItemsAcceptsAPIURLWithoutExtension(t *testing.T) {
	items := []VideoItem{{
		VideoURL: "https://v26-default.douyinvod.com/abc123",
	}}
	got := filterSSEVideoItems(items)
	if len(got) != 1 {
		t.Fatalf("SSE video_url without extension should be kept, got %#v", got)
	}
}

func TestChatEditorQueryJSCoversCurrentDoubaoComposer(t *testing.T) {
	js := chatEditorQueryJS
	for _, needle := range []string{
		"textarea.semi-input-textarea",
		".tiptap.ProseMirror",
		`[contenteditable="plaintext-only"]`,
		"paintedChatEl",
		"chatEditorRect",
		"document.activeElement",
		"raw=[",
		"pickChatEditor",
	} {
		if !strings.Contains(js, needle) {
			t.Fatalf("chatEditorQueryJS missing %q", needle)
		}
	}
	if strings.Contains(js, "offsetParent") {
		t.Fatal("chat editor finder must not use offsetParent (breaks position:fixed composers)")
	}
	if strings.Contains(js, `closest('[hidden], [aria-hidden="true"]')`) {
		t.Fatal("must not skip ancestor aria-hidden (slash/upload popovers)")
	}
	if strings.Contains(js, `getAttribute('aria-hidden') === 'true'`) {
		t.Fatal("must not reject the editor's transient aria-hidden state")
	}
}

func TestIsRetryableComposerError(t *testing.T) {
	for _, msg := range []string{
		"focus editor: chat editor not found",
		"refuse video generation: prompt mismatch in editor",
		"Input.insertText: target lost",
	} {
		if !isRetryableComposerError(errors.New(msg)) {
			t.Fatalf("expected retryable composer error: %q", msg)
		}
	}
	if isRetryableComposerError(errors.New("video generation quota exceeded")) {
		t.Fatal("quota errors must not rebuild and resubmit")
	}
}

func TestMaterialSafetyConfirmationText(t *testing.T) {
	text := "安全确认 你在本功能中上传、使用的素材，均已获充分授权，无侵权违法风险。相关责任需由你自行承担。"
	if !materialSafetyConfirmationText(text) {
		t.Fatal("expected material safety confirmation copy to match")
	}
	if materialSafetyConfirmationText("请确认以下视频生成参数") {
		t.Fatal("ordinary video confirmation must not match legal safety dialog")
	}
}
