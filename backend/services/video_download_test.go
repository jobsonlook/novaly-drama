package services

import (
	"net/url"
	"testing"
)

func TestVideoDownloadCandidatesPrefersUnwrappedCDN(t *testing.T) {
	cdn := "https://v3-dy.ixigua.com/video.mp4?lr=unwatermarked"
	proxy := "http://127.0.0.1:8086/api/v3/images/proxy?url=" + url.QueryEscape(cdn)
	got := videoDownloadCandidates(proxy)
	if len(got) != 2 || got[0] != cdn || got[1] != proxy {
		t.Fatalf("candidates=%v", got)
	}
}

func TestVideoDownloadCandidatesUnwrapsVideoProxy(t *testing.T) {
	cdn := "https://v26-default.heycan.com/foo.mp4"
	proxy := "http://127.0.0.1:8086/api/v3/videos/proxy?url=" + url.QueryEscape(cdn)
	if unwrapDoubaoProxyURL(proxy) != cdn {
		t.Fatalf("unwrap=%q", unwrapDoubaoProxyURL(proxy))
	}
	got := videoDownloadCandidates(proxy)
	if len(got) != 2 || got[0] != cdn {
		t.Fatalf("candidates=%v", got)
	}
}

func TestVideoDownloadCandidatesPlainURL(t *testing.T) {
	raw := "https://example.com/a.mp4"
	got := videoDownloadCandidates(raw)
	if len(got) != 1 || got[0] != raw {
		t.Fatalf("candidates=%v", got)
	}
}
