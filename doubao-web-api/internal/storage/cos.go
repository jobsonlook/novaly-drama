package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type COSConfig struct {
	SecretID      string
	SecretKey     string
	Bucket        string
	Region        string
	PublicBaseURL string
	Accelerate    bool
	KeyPrefix     string // e.g. doubao-web/videos
}

func (c COSConfig) Enabled() bool {
	return strings.TrimSpace(c.SecretID) != "" &&
		strings.TrimSpace(c.SecretKey) != "" &&
		strings.TrimSpace(c.Bucket) != "" &&
		strings.TrimSpace(c.Region) != ""
}

type COS struct {
	cfg    COSConfig
	client *cos.Client
	base   string
}

func NewCOS(cfg COSConfig) (*COS, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	rawURL := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Region)
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse COS URL: %w", err)
	}
	transport := &cos.AuthorizationTransport{
		SecretID:  cfg.SecretID,
		SecretKey: cfg.SecretKey,
	}
	bucketURL := u
	if cfg.Accelerate {
		au, err := url.Parse(fmt.Sprintf("https://%s.cos.accelerate.myqcloud.com", cfg.Bucket))
		if err != nil {
			return nil, fmt.Errorf("parse COS accelerate URL: %w", err)
		}
		bucketURL = au
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Timeout:   5 * time.Minute,
		Transport: transport,
	})
	base := strings.TrimSuffix(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if base == "" {
		base = rawURL
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "doubao-web/videos"
	}
	return &COS{cfg: cfg, client: client, base: base}, nil
}

func (c *COS) Enabled() bool {
	return c != nil && c.client != nil
}

func (c *COS) PublicURL(key string) string {
	key = strings.TrimPrefix(key, "/")
	return c.base + "/" + key
}

func (c *COS) IsPublicURL(u string) bool {
	if !c.Enabled() || u == "" {
		return false
	}
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, c.base+"/") {
		return true
	}
	host := fmt.Sprintf("%s.cos.%s.myqcloud.com", c.cfg.Bucket, c.cfg.Region)
	acc := fmt.Sprintf("%s.cos.accelerate.myqcloud.com", c.cfg.Bucket)
	return strings.Contains(u, host) || strings.Contains(u, acc)
}

func (c *COS) Put(key string, data []byte, contentType string) error {
	if !c.Enabled() {
		return fmt.Errorf("COS not configured")
	}
	key = strings.TrimPrefix(key, "/")
	if contentType == "" {
		contentType = "video/mp4"
		switch strings.ToLower(path.Ext(key)) {
		case ".webm":
			contentType = "video/webm"
		case ".mov":
			contentType = "video/quicktime"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// inline so browsers play video/mp4 in-tab instead of forcing a download.
	disposition := "inline"
	if ext := strings.ToLower(path.Ext(key)); ext != "" {
		disposition = `inline; filename="` + path.Base(key) + `"`
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:        contentType,
			ContentDisposition: disposition,
		},
		ACLHeaderOptions: &cos.ACLHeaderOptions{XCosACL: "public-read"},
	}
	_, err := c.client.Object.Put(ctx, key, bytes.NewReader(data), opt)
	if err != nil {
		return fmt.Errorf("COS put %s: %w", key, err)
	}
	return nil
}

func (c *COS) VideoKey(taskID string) string {
	prefix := strings.Trim(c.cfg.KeyPrefix, "/")
	now := time.Now()
	return fmt.Sprintf("%s/%04d/%02d/%s.mp4", prefix, now.Year(), int(now.Month()), taskID)
}
