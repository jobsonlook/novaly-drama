package services

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const maxVideoDownloadBytes = 120 << 20

// ValidateDownloadedVideo rejects truncated Douyin/Doubao MP4s that look like
// a video (ftyp) but cannot be played because the moov index never arrived.
func ValidateDownloadedVideo(data []byte, contentLength int64) error {
	if len(data) < 32 {
		return fmt.Errorf("下载视频过短（%d 字节），不是完整成片", len(data))
	}
	if contentLength > 0 && int64(len(data)) < contentLength {
		return fmt.Errorf("下载被截断：收到 %d / %d 字节", len(data), contentLength)
	}
	if contentLength > maxVideoDownloadBytes && int64(len(data)) >= maxVideoDownloadBytes {
		return fmt.Errorf("视频超过本地下载上限 %dMB", maxVideoDownloadBytes>>20)
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xd8}) || bytes.HasPrefix(data, []byte("\x89PNG")) || bytes.HasPrefix(data, []byte("GIF")) {
		return fmt.Errorf("下载到的是图片封面，不是视频文件")
	}
	trim := bytes.TrimSpace(data)
	if bytes.HasPrefix(trim, []byte("<")) || bytes.HasPrefix(trim, []byte("{")) {
		return fmt.Errorf("下载到的不是视频文件")
	}
	if len(data) >= 8 && string(data[4:8]) == "ftyp" && !mp4HasMoov(data) {
		return fmt.Errorf("视频文件不完整（缺少 moov 索引，下载被截断）")
	}
	return nil
}

func mp4HasMoov(data []byte) bool {
	i := 0
	for i+8 <= len(data) {
		size := uint64(binary.BigEndian.Uint32(data[i : i+4]))
		typ := string(data[i+4 : i+8])
		header := uint64(8)
		if size == 1 {
			if i+16 > len(data) {
				return false
			}
			size = binary.BigEndian.Uint64(data[i+8 : i+16])
			header = 16
		}
		if typ == "moov" {
			return true
		}
		if size < header {
			return false
		}
		next := i + int(size)
		if next <= i || next > len(data) {
			return false
		}
		i = next
	}
	return false
}
