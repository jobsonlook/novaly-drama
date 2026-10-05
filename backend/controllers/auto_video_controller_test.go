package controllers

import (
	"strings"
	"testing"
)

func TestDialogueLinesAndNormalize(t *testing.T) {
	got := dialogueLines(`镜头一；小满说 {石头脏，为什么怪小孩？}。音效 <风声>；执事说 {住手！}`)
	if len(got) != 2 || got[0] != "石头脏，为什么怪小孩？" || got[1] != "住手！" {
		t.Fatalf("unexpected dialogue: %#v", got)
	}
	if normalizeSpeech("石头脏， 为什么怪小孩？") != "石头脏为什么怪小孩" {
		t.Fatal("speech normalization should ignore punctuation and spaces")
	}
}

func TestAutoVideoPauseFailedReason(t *testing.T) {
	got := autoVideoPauseFailedReason(8, 2, "信封文字写成了陌元")
	if !strings.Contains(got, "第 8 镜连续 2 个版本") || !strings.Contains(got, "陌元") {
		t.Fatalf("unexpected reason: %s", got)
	}
}

func TestIsKeepVideoTaskError(t *testing.T) {
	if !isKeepVideoTaskError(errString("视频生成超时（豆包排队常需 15 分钟以上），请稍后在任务中重试或到豆包页确认是否已生成")) {
		t.Fatal("timeout should keep the Doubao task id")
	}
	if !isKeepVideoTaskError(errString("video UI mode timed out — check Chrome tab")) {
		t.Fatal("cdp timeout should keep the Doubao task id")
	}
	if isKeepVideoTaskError(errString("quota_exhausted")) {
		t.Fatal("quota errors should start a new generation")
	}
}

func TestIsRetryableVideoDownloadMessage(t *testing.T) {
	if !isRetryableVideoDownloadMessage("自动审核失败：视频无法解码：exit status 1") {
		t.Fatal("decode pause should keep the Doubao task for re-download")
	}
	if !isRetryableVideoDownloadMessage("视频文件不完整（下载被截断，豆包页能播但本地文件缺索引）") {
		t.Fatal("truncated download should be retryable")
	}
	if isRetryableVideoDownloadMessage("信封文字写成了陌元") {
		t.Fatal("visual QC failure should start a new generation")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRatioMatches(t *testing.T) {
	for _, tc := range []struct {
		ratio string
		w, h  int
		want  bool
	}{
		{"9:16", 720, 1280, true},
		{"16:9", 1920, 1080, true},
		{"9:16", 1920, 1080, false},
	} {
		if got := ratioMatches(tc.ratio, tc.w, tc.h); got != tc.want {
			t.Fatalf("ratioMatches(%q,%d,%d)=%v want %v", tc.ratio, tc.w, tc.h, got, tc.want)
		}
	}
}
