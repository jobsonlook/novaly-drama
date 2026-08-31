package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type COSConfig struct {
	SecretID      string
	SecretKey     string
	Bucket        string
	Region        string
	PublicBaseURL string // optional CDN / custom domain
	Accelerate    bool   // use cos.accelerate for uploads (全球加速)
}

func (c COSConfig) Enabled() bool {
	return strings.TrimSpace(c.SecretID) != "" &&
		strings.TrimSpace(c.SecretKey) != "" &&
		strings.TrimSpace(c.Bucket) != "" &&
		strings.TrimSpace(c.Region) != ""
}

type COSStorage struct {
	cfg          COSConfig
	client       *cos.Client // regional: get/copy/exists
	uploadClient *cos.Client // accelerate (or regional) for browser PUT
	base         string
}

func NewCOSStorage(cfg COSConfig) (*COSStorage, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	rawURL := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Region)
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析 COS URL 失败：%w", err)
	}
	transport := &cos.AuthorizationTransport{
		SecretID:  cfg.SecretID,
		SecretKey: cfg.SecretKey,
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Timeout:   10 * time.Minute,
		Transport: transport,
	})
	uploadClient := client
	if cfg.Accelerate {
		au, err := url.Parse(fmt.Sprintf("https://%s.cos.accelerate.myqcloud.com", cfg.Bucket))
		if err != nil {
			return nil, fmt.Errorf("解析 COS 加速域名失败：%w", err)
		}
		uploadClient = cos.NewClient(&cos.BaseURL{BucketURL: au}, &http.Client{
			Timeout:   10 * time.Minute,
			Transport: transport,
		})
	}
	base := strings.TrimSuffix(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if base == "" {
		base = rawURL
	}
	return &COSStorage{cfg: cfg, client: client, uploadClient: uploadClient, base: base}, nil
}

func (c *COSStorage) Enabled() bool {
	return c != nil && c.client != nil && c.uploadClient != nil
}

func (c *COSStorage) UsingAccelerate() bool {
	return c != nil && c.cfg.Accelerate
}

func (c *COSStorage) PublicURL(key string) string {
	key = strings.TrimPrefix(key, "/")
	return c.base + "/" + key
}

// KeyFromPublicURL extracts the object key if u points at this bucket (regional, accelerate, or PublicBaseURL).
func (c *COSStorage) KeyFromPublicURL(u string) (string, bool) {
	if !c.Enabled() || u == "" {
		return "", false
	}
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	bases := []string{
		c.base + "/",
		fmt.Sprintf("https://%s.cos.%s.myqcloud.com/", c.cfg.Bucket, c.cfg.Region),
		fmt.Sprintf("https://%s.cos.accelerate.myqcloud.com/", c.cfg.Bucket),
	}
	for _, b := range bases {
		if strings.HasPrefix(u, b) {
			key := strings.TrimPrefix(u, b)
			key = strings.TrimPrefix(key, "/")
			if key != "" {
				return key, true
			}
		}
	}
	return "", false
}

// PresignPut returns a time-limited PUT URL for browser direct upload.
func (c *COSStorage) PresignPut(key, contentType string, expire time.Duration) (string, map[string]string, error) {
	if !c.Enabled() {
		return "", nil, fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	if contentType == "" {
		contentType = contentTypeForKey(key)
	}
	if expire <= 0 {
		expire = 30 * time.Minute
	}
	header := &http.Header{}
	header.Set("Content-Type", contentType)
	header.Set("x-cos-acl", "public-read")
	// Browser PUT must use regional endpoint: accelerate domain returns 400 when 全球加速 is not enabled,
	// which surfaces as CORS "Failed to fetch" in the browser.
	u, err := c.client.Object.GetPresignedURL(
		context.Background(),
		http.MethodPut,
		key,
		c.cfg.SecretID,
		c.cfg.SecretKey,
		expire,
		&cos.PresignedURLOptions{Header: header},
	)
	if err != nil {
		return "", nil, fmt.Errorf("生成 COS 上传签名失败：%w", err)
	}
	return u.String(), map[string]string{
		"Content-Type": contentType,
		"x-cos-acl":    "public-read",
	}, nil
}

func (c *COSStorage) InitiateMultipart(key, contentType string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	if contentType == "" {
		contentType = contentTypeForKey(key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, _, err := c.client.Object.InitiateMultipartUpload(ctx, key, &cos.InitiateMultipartUploadOptions{
		ACLHeaderOptions: &cos.ACLHeaderOptions{XCosACL: "public-read"},
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return "", fmt.Errorf("初始化分片上传失败：%w", err)
	}
	if res == nil || res.UploadID == "" {
		return "", fmt.Errorf("初始化分片上传失败：未返回 UploadId")
	}
	return res.UploadID, nil
}

func (c *COSStorage) PresignPart(key, uploadID string, partNumber int, expire time.Duration) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	if partNumber < 1 {
		return "", fmt.Errorf("无效分片号")
	}
	if expire <= 0 {
		expire = 30 * time.Minute
	}
	q := &url.Values{}
	q.Set("partNumber", strconv.Itoa(partNumber))
	q.Set("uploadId", uploadID)
	u, err := c.client.Object.GetPresignedURL(
		context.Background(),
		http.MethodPut,
		key,
		c.cfg.SecretID,
		c.cfg.SecretKey,
		expire,
		&cos.PresignedURLOptions{Query: q},
	)
	if err != nil {
		return "", fmt.Errorf("生成分片签名失败：%w", err)
	}
	return u.String(), nil
}

type MultipartPart struct {
	PartNumber int
	ETag       string
}

func (c *COSStorage) CompleteMultipart(key, uploadID string, parts []MultipartPart) error {
	if !c.Enabled() {
		return fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	objs := make([]cos.Object, 0, len(parts))
	for _, p := range parts {
		etag := strings.Trim(p.ETag, `"`)
		objs = append(objs, cos.Object{PartNumber: p.PartNumber, ETag: etag})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	opt := &cos.CompleteMultipartUploadOptions{Parts: objs}
	_, _, err := c.client.Object.CompleteMultipartUpload(ctx, key, uploadID, opt)
	if err != nil {
		return fmt.Errorf("合并分片失败：%w", err)
	}
	return nil
}

func (c *COSStorage) AbortMultipart(key, uploadID string) {
	if !c.Enabled() || uploadID == "" {
		return
	}
	key = strings.TrimPrefix(key, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = c.client.Object.AbortMultipartUpload(ctx, key, uploadID)
}

func (c *COSStorage) Put(key string, data []byte, contentType string) error {
	if !c.Enabled() {
		return fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	if contentType == "" {
		contentType = contentTypeForKey(key)
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
		ACLHeaderOptions: &cos.ACLHeaderOptions{
			XCosACL: "public-read",
		},
	}
	// Prefer accelerate client for server-side uploads when configured (same as browser PUT).
	client := c.uploadClient
	if client == nil {
		client = c.client
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		_, err := client.Object.Put(ctx, key, bytes.NewReader(data), opt)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		msg := err.Error()
		retryable := strings.Contains(msg, "deadline exceeded") ||
			strings.Contains(msg, "Timeout") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "i/o timeout")
		if !retryable || attempt == 3 {
			break
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	return fmt.Errorf("上传到 COS 失败 (%s)：%w", key, lastErr)
}

// Copy duplicates an existing object within the same bucket (avoids re-uploading large videos).
func (c *COSStorage) Copy(srcKey, dstKey string) error {
	if !c.Enabled() {
		return fmt.Errorf("COS 未配置")
	}
	srcKey = strings.TrimPrefix(srcKey, "/")
	dstKey = strings.TrimPrefix(dstKey, "/")
	sourceURL := fmt.Sprintf("%s.cos.%s.myqcloud.com/%s", c.cfg.Bucket, c.cfg.Region, srcKey)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _, err := c.client.Object.Copy(ctx, dstKey, sourceURL, &cos.ObjectCopyOptions{
		ObjectCopyHeaderOptions: &cos.ObjectCopyHeaderOptions{
			XCosMetadataDirective: "Copy",
		},
		ACLHeaderOptions: &cos.ACLHeaderOptions{
			XCosACL: "public-read",
		},
	})
	if err != nil {
		return fmt.Errorf("COS 复制失败 (%s → %s)：%w", srcKey, dstKey, err)
	}
	return nil
}

func (c *COSStorage) Get(key string) ([]byte, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := c.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ObjectSize returns Content-Length via HEAD (no body download).
func (c *COSStorage) ObjectSize(key string) (int64, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("COS 未配置")
	}
	key = strings.TrimPrefix(key, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := c.client.Object.Head(ctx, key, nil)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, fmt.Errorf("COS Head 无响应：%s", key)
	}
	return resp.ContentLength, nil
}

func (c *COSStorage) Delete(key string) error {
	if !c.Enabled() {
		return nil
	}
	key = strings.TrimPrefix(key, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.client.Object.Delete(ctx, key)
	return err
}

func (c *COSStorage) Exists(key string) bool {
	if !c.Enabled() {
		return false
	}
	key = strings.TrimPrefix(key, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, err := c.client.Object.IsExist(ctx, key)
	return err == nil && ok
}

func contentTypeForKey(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".m4v":
		return "video/x-m4v"
	default:
		return "application/octet-stream"
	}
}
