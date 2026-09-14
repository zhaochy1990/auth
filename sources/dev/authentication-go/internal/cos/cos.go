// Package cos wraps the Tencent Cloud COS object-storage API (the official
// cos-go-sdk-v5, NOT the tencentcloud-sdk-go Cloud-API module). It mirrors the
// internal/sms client: request signing, upload, and error mapping to the
// apperror vocabulary. COS credentials stay server-side; the mini-program only
// ever sees the resulting public avatar URL.
//
// The client is configured from global config (TENCENT_COS_*); missing
// credentials do not prevent startup — every call returns `cos_not_configured`
// in that case.
package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	cosgo "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/x/logger"
)

// Config holds the Tencent Cloud COS credentials and bucket targeting.
type Config struct {
	SecretID  string
	SecretKey string
	Bucket    string
	Region    string
	// BaseURL is the public/CDN endpoint used to build the returned avatar_url
	// (e.g. https://stride-running-1255867366.cos.ap-shanghai.myqcloud.com,
	// or the CDN domain once one is added). Distinct from the upload endpoint.
	BaseURL string
}

// Configured reports whether every credential needed to upload is present.
func (c Config) Configured() bool {
	if c.SecretID == "" {
		logger.S().Warn("Tencent Cloud COS SecretID is empty; avatar upload will return cos_not_configured")
	}
	if c.SecretKey == "" {
		logger.S().Warn("Tencent Cloud COS SecretKey is empty; avatar upload will return cos_not_configured")
	}
	if c.Bucket == "" {
		logger.S().Warn("Tencent Cloud COS Bucket is empty; avatar upload will return cos_not_configured")
	}
	if c.Region == "" {
		logger.S().Warn("Tencent Cloud COS Region is empty; avatar upload will return cos_not_configured")
	}
	if c.BaseURL == "" {
		logger.S().Warn("Tencent Cloud COS BaseURL is empty; avatar upload will return cos_not_configured")
	}
	return c.SecretID != "" && c.SecretKey != "" && c.Bucket != "" && c.Region != "" && c.BaseURL != ""
}

// Client uploads objects to Tencent Cloud COS.
type Client struct {
	cfg Config
	cli *cosgo.Client
}

// NewClient creates a COS client. bucketURL overrides the upload endpoint
// (derived from Bucket+Region when nil) — used by tests to point at a fake COS
// server. An unconfigured Config keeps the client present but makes Upload
// return a "not configured" error.
func NewClient(cfg Config, bucketURL *url.URL) *Client {
	httpCli := &http.Client{Timeout: 30 * time.Second}
	if cfg.Configured() {
		httpCli.Transport = &cosgo.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
			Transport: &http.Transport{},
		}
	}
	if bucketURL == nil && cfg.Bucket != "" && cfg.Region != "" {
		u, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Region))
		if err == nil {
			bucketURL = u
		}
	}
	var cli *cosgo.Client
	if bucketURL != nil {
		cli = cosgo.NewClient(&cosgo.BaseURL{BucketURL: bucketURL}, httpCli)
	}
	return &Client{cfg: cfg, cli: cli}
}

// Configured reports whether the client has every credential needed to upload.
func (c *Client) Configured() bool { return c.cfg.Configured() }

// Upload writes an object at key. contentType is the object's stored MIME type.
func (c *Client) Upload(ctx context.Context, key string, r io.Reader, contentType string) error {
	if !c.cfg.Configured() || c.cli == nil {
		return apperror.CosNotConfigured()
	}
	_, err := c.cli.Object.Put(ctx, key, r, &cosgo.ObjectPutOptions{
		ObjectPutHeaderOptions: &cosgo.ObjectPutHeaderOptions{ContentType: contentType},
	})
	if err != nil {
		logger.S().Errorw("COS upload failed", "key", key, "err", err)
		return apperror.CosProviderError("Tencent Cloud COS upload failed")
	}
	return nil
}

// PublicURL returns the publicly-reachable URL for key.
func (c *Client) PublicURL(key string) string {
	return c.cfg.BaseURL + "/" + key
}

// Delete removes the object at key. It is used during account erasure to clear
// the user's avatar. A call on an unconfigured client returns cos_not_configured
// so the caller can decide whether that is fatal.
func (c *Client) Delete(ctx context.Context, key string) error {
	if !c.cfg.Configured() || c.cli == nil {
		return apperror.CosNotConfigured()
	}
	if _, err := c.cli.Object.Delete(ctx, key); err != nil {
		logger.S().Errorw("COS delete failed", "key", key, "err", err)
		return apperror.CosProviderError("Tencent Cloud COS delete failed")
	}
	return nil
}
