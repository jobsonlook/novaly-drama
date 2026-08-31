package services

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
)

type TOSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Region          string
	Endpoint        string
}

func (c TOSConfig) Enabled() bool {
	return c.AccessKeyID != "" && c.SecretAccessKey != "" && c.Bucket != "" && c.Endpoint != "" && c.Region != ""
}

type TOSStorage struct {
	bucket string
	client *tos.ClientV2
}

func NewTOSStorage(cfg TOSConfig) (*TOSStorage, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	endpoint := cfg.Endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	client, err := tos.NewClientV2(
		endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey)),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 TOS 客户端失败：%w", err)
	}
	return &TOSStorage{bucket: cfg.Bucket, client: client}, nil
}

func (t *TOSStorage) Enabled() bool {
	return t != nil && t.client != nil
}

func (t *TOSStorage) UploadPixAPIRef(projectID uint, data []byte, ext string) (string, error) {
	if !t.Enabled() {
		return "", fmt.Errorf("TOS 未配置")
	}
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "jpg"
	}
	contentType := "image/jpeg"
	switch ext {
	case "png":
		contentType = "image/png"
	case "webp":
		contentType = "image/webp"
	case "gif":
		contentType = "image/gif"
	}
	key := fmt.Sprintf("pixapi/refs/p%d/%d.%s", projectID, time.Now().UnixNano(), ext)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := t.client.PutObjectV2(ctx, &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket:      t.bucket,
			Key:         key,
			ContentType: contentType,
		},
		Content: bytes.NewReader(data),
	})
	if err != nil {
		return "", fmt.Errorf("上传参考图到 TOS 失败：%w", err)
	}
	signed, err := t.client.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodGet,
		Bucket:     t.bucket,
		Key:        key,
		Expires:    3600,
	})
	if err != nil {
		return "", fmt.Errorf("生成 TOS 访问链接失败：%w", err)
	}
	if signed.SignedUrl == "" {
		return "", fmt.Errorf("TOS 未返回访问链接")
	}
	return signed.SignedUrl, nil
}
