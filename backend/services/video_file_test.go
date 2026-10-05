package services

import (
	"encoding/binary"
	"testing"
)

func mp4Box(typ string, payload []byte) []byte {
	n := 8 + len(payload)
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b, uint32(n))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func TestValidateDownloadedVideoCompleteMP4(t *testing.T) {
	data := append(mp4Box("ftyp", []byte("isomiso2avc1mp41")), mp4Box("moov", make([]byte, 16))...)
	if err := ValidateDownloadedVideo(data, int64(len(data))); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDownloadedVideoTruncatedMP4(t *testing.T) {
	data := append(mp4Box("ftyp", []byte("isom")), mp4Box("mdat", make([]byte, 64))...)
	err := ValidateDownloadedVideo(data, 0)
	if err == nil || err.Error() != "视频文件不完整（缺少 moov 索引，下载被截断）" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateDownloadedVideoShortContentLength(t *testing.T) {
	data := append(mp4Box("ftyp", []byte("isom")), mp4Box("moov", []byte("xxxx"))...)
	err := ValidateDownloadedVideo(data, int64(len(data)+1000))
	if err == nil {
		t.Fatal("expected truncated content-length error")
	}
}

func TestValidateDownloadedVideoRejectsJPEG(t *testing.T) {
	data := append([]byte{0xff, 0xd8, 0xff}, make([]byte, 40)...)
	if err := ValidateDownloadedVideo(data, 0); err == nil {
		t.Fatal("jpeg cover must be rejected")
	}
}
