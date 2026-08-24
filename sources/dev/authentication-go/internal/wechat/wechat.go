// Package wechat wraps the WeChat mini-program login API (code2Session).
package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

const code2SessionURL = "https://api.weixin.qq.com/sns/jscode2session"

// SessionResult is a successful code2Session response.
type SessionResult struct {
	OpenID     string  `json:"openid"`
	SessionKey string  `json:"session_key"`
	UnionID    *string `json:"unionid,omitempty"`
}

// Client calls the WeChat mini-program API.
type Client struct {
	appID     string
	appSecret string
	endpoint  string
	httpCli   *http.Client
}

// NewClient creates a WeChat API client. endpoint overrides the default WeChat
// API URL (used by tests to point at a fake server); pass "" for production.
// Empty appID or appSecret disables the client — all calls return a
// "not configured" error.
func NewClient(appID, appSecret, endpoint string) *Client {
	if endpoint == "" {
		endpoint = code2SessionURL
	}
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		endpoint:  endpoint,
		httpCli:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Code2Session exchanges a wx.login() code for openid / session_key / unionid.
func (c *Client) Code2Session(ctx context.Context, code string) (*SessionResult, error) {
	if c.appID == "" || c.appSecret == "" {
		return nil, apperror.BadRequest("WeChat login is not configured")
	}
	if code == "" {
		return nil, apperror.BadRequest("code is required")
	}
	reqURL := fmt.Sprintf("%s?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		c.endpoint, url.QueryEscape(c.appID), url.QueryEscape(c.appSecret), url.QueryEscape(code))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, wechatAPIError("could not build request")
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, wechatAPIError("request failed")
	}
	defer resp.Body.Close()

	var raw struct {
		OpenID     string  `json:"openid"`
		SessionKey string  `json:"session_key"`
		UnionID    *string `json:"unionid,omitempty"`
		ErrCode    int     `json:"errcode"`
		ErrMsg     string  `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, wechatAPIError("malformed response")
	}
	if raw.ErrCode != 0 {
		return nil, mapWechatError(raw.ErrCode, raw.ErrMsg)
	}
	if raw.OpenID == "" {
		return nil, wechatAPIError("no openid in response")
	}
	return &SessionResult{
		OpenID:     raw.OpenID,
		SessionKey: raw.SessionKey,
		UnionID:    raw.UnionID,
	}, nil
}

// wechatAPIError returns a client-facing 400 for any code2Session failure
// (network, malformed response, or an unclassifiable WeChat error). The spec
// treats WeChat API failures the same as an invalid code: HTTP 400 with a
// stable error code.
func wechatAPIError(detail string) error {
	return apperror.New(400, "wechat_api_error", "WeChat API error: "+detail)
}

// mapWechatError translates a WeChat errcode to an apperror.
// Common codes: 40029 invalid code, 45011 rate limited, 40013 invalid appid.
func mapWechatError(code int, msg string) error {
	switch code {
	case 40029:
		return apperror.New(400, "wechat_invalid_code", "Invalid WeChat code")
	case 45011:
		return apperror.New(429, "wechat_rate_limited", "WeChat API rate limit exceeded")
	case 40013:
		return apperror.New(400, "wechat_invalid_appid", "WeChat appid is invalid")
	default:
		return apperror.New(400, "wechat_api_error",
			fmt.Sprintf("WeChat API error: %s", msg))
	}
}
