package doubao

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

type UploadResult struct {
	URI    string `json:"uri"`
	URL    string `json:"url"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func (c *Client) UploadImage(ctx context.Context, data []byte, filename string) (UploadResult, error) {
	if c.upload == nil {
		return UploadResult{}, fmt.Errorf("image upload not configured")
	}
	if len(data) == 0 {
		return UploadResult{}, fmt.Errorf("empty image data")
	}
	if filename == "" {
		filename = "image.png"
	}
	return c.upload(ctx, data, filename)
}

// ExtractRefImageKey reads reference image uri from GenerateImagesRequest.Image.
// Supports: uri string, url string (must upload first), data URI base64 (auto-upload).
func (c *Client) ExtractRefImageKey(ctx context.Context, image any) (string, error) {
	if image == nil {
		return "", nil
	}

	switch v := image.(type) {
	case string:
		return c.resolveImageRef(ctx, v)
	case *string:
		if v == nil {
			return "", nil
		}
		return c.resolveImageRef(ctx, *v)
	case []string:
		if len(v) == 0 {
			return "", nil
		}
		return c.resolveImageRef(ctx, v[0])
	case []any:
		if len(v) == 0 {
			return "", nil
		}
		if s, ok := v[0].(string); ok {
			return c.resolveImageRef(ctx, s)
		}
	}
	return "", fmt.Errorf("unsupported image field type %T", image)
}

func (c *Client) resolveImageRef(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "data:image/") {
		data, filename, err := decodeDataURI(value)
		if err != nil {
			return "", err
		}
		result, err := c.UploadImage(ctx, data, filename)
		if err != nil {
			return "", err
		}
		return result.URI, nil
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return "", fmt.Errorf("image url is not supported directly, upload via POST /api/v3/images/uploads first")
	}
	// treat as uploaded uri/key
	return value, nil
}

func decodeDataURI(dataURI string) ([]byte, string, error) {
	comma := strings.Index(dataURI, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("invalid data uri")
	}
	meta := dataURI[:comma]
	payload := dataURI[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return nil, "", fmt.Errorf("only base64 data uri is supported")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", fmt.Errorf("decode data uri: %w", err)
	}
	ext := "png"
	if i := strings.Index(meta, "image/"); i >= 0 {
		sub := meta[i+len("image/"):]
		if j := strings.Index(sub, ";"); j >= 0 {
			sub = sub[:j]
		}
		if sub != "" {
			ext = sub
		}
	}
	return data, "image." + ext, nil
}

func (c *Client) UploadMedia(ctx context.Context, data []byte, filename string) (UploadResult, error) {
	if c.upload == nil {
		return UploadResult{}, fmt.Errorf("file upload not configured")
	}
	if len(data) == 0 {
		return UploadResult{}, fmt.Errorf("empty file data")
	}
	if filename == "" {
		filename = "file.bin"
	}
	return c.upload(ctx, data, filename)
}

func (c *Client) resolveMediaRef(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "data:audio/") {
		data, filename, err := decodeAudioDataURI(value)
		if err != nil {
			return "", err
		}
		result, err := c.UploadMedia(ctx, data, filename)
		if err != nil {
			return "", err
		}
		return result.URI, nil
	}
	if strings.HasPrefix(value, "data:image/") {
		return c.resolveImageRef(ctx, value)
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return "", fmt.Errorf("media url is not supported directly, upload via POST /api/v3/files/uploads first")
	}
	return value, nil
}

func decodeAudioDataURI(dataURI string) ([]byte, string, error) {
	comma := strings.Index(dataURI, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("invalid data uri")
	}
	meta := dataURI[:comma]
	payload := dataURI[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return nil, "", fmt.Errorf("only base64 data uri is supported")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", fmt.Errorf("decode data uri: %w", err)
	}
	ext := "mp3"
	if i := strings.Index(meta, "audio/"); i >= 0 {
		sub := meta[i+len("audio/"):]
		if j := strings.Index(sub, ";"); j >= 0 {
			sub = sub[:j]
		}
		if sub != "" {
			ext = sub
		}
	}
	return data, "audio." + ext, nil
}

func MediaExt(filename string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif", "bmp":
		return ext
	case "mp3", "wav", "m4a", "aac", "ogg":
		return ext
	default:
		return ext
	}
}

const localAudioPrefix = "local-audio:"

func IsLocalAudioRef(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), localAudioPrefix)
}

func IsAudioFile(filename string) bool {
	switch MediaExt(filename) {
	case "mp3", "wav", "m4a", "aac", "ogg":
		return true
	default:
		return false
	}
}

func ImageExt(filename string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		return "png"
	}
	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif", "bmp":
		return ext
	default:
		return "png"
	}
}
