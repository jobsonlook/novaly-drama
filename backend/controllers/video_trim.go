package controllers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func needsExactDoubaoTrim(seconds int) bool {
	if seconds <= 0 || seconds > 15 {
		return false
	}
	return seconds != 5 && seconds != 10 && seconds != 15
}

func trimVideoBytes(data []byte, seconds int) ([]byte, error) {
	if len(data) == 0 || !needsExactDoubaoTrim(seconds) {
		return data, nil
	}
	dir, err := os.MkdirTemp("", "novaly-video-trim-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "input.mp4")
	outPath := filepath.Join(dir, "output.mp4")
	if err := os.WriteFile(inPath, data, 0o600); err != nil {
		return nil, err
	}
	// Re-encode so the MP4 timeline ends at the requested frame. Stream-copy
	// trimming stops on packet/keyframe boundaries (for example 7s became
	// 7.083333s in a real Seedance file).
	cmd := exec.Command("ffmpeg", "-y", "-i", inPath, "-t", strconv.Itoa(seconds),
		"-map", "0:v:0", "-map", "0:a?", "-c:v", "libx264", "-preset", "veryfast",
		"-crf", "18", "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", outPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg 裁剪失败: %w (%s)", err, string(output))
	}
	trimmed, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("ffmpeg 裁剪后文件为空")
	}
	return trimmed, nil
}
