// Package sms wraps the Tencent Cloud SMS SendSms API (TC3-HMAC-SHA256
// signing). It mirrors the internal/wechat client: request signing, HTTP call,
// response parsing, and error mapping to the apperror vocabulary.
//
// The client is configured from global environment variables (TENCENT_SMS_*);
// missing credentials do not prevent startup — every call returns
// `sms_not_configured` in that case.
package sms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

const (
	defaultEndpoint = "https://sms.tencentcloudapi.com"
	serviceName     = "sms"
	actionName      = "SendSms"
	apiVersion      = "2021-01-11"
	contentType     = "application/json; charset=utf-8"
)

// Config holds the Tencent Cloud SMS credentials (global, from env).
type Config struct {
	SecretID   string
	SecretKey  string
	SDKAppID   string
	SignName   string
	TemplateID string
	Region     string
}

// Configured reports whether every credential needed to send is present.
func (c Config) Configured() bool {
	return c.SecretID != "" && c.SecretKey != "" && c.SDKAppID != "" && c.SignName != "" && c.TemplateID != ""
}

// Client calls the Tencent Cloud SMS API.
type Client struct {
	cfg      Config
	endpoint string
	httpCli  *http.Client
}

// NewClient creates a Tencent SMS API client. endpoint overrides the default
// Tencent endpoint (used by tests to point at a fake server); pass "" for
// production. An unconfigured Config makes every call return a
// "not configured" error.
func NewClient(cfg Config, endpoint string) *Client {
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return &Client{cfg: cfg, endpoint: endpoint, httpCli: &http.Client{Timeout: 10 * time.Second}}
}

// Configured reports whether the client has every credential needed to send.
func (c *Client) Configured() bool { return c.cfg.Configured() }

// SendCode delivers a verification code to a mainland-China phone number
// (bare 11 digits; the client prefixes +86). Template params are
// {1}=code, {2}=validity minutes (the approved 上海砺跑科技 template 2716979).
func (c *Client) SendCode(ctx context.Context, phone, code string) error {
	if !c.cfg.Configured() {
		return apperror.SmsNotConfigured()
	}
	payload, err := json.Marshal(map[string]any{
		"PhoneNumberSet":   []string{"+86" + phone},
		"SmsSdkAppId":      c.cfg.SDKAppID,
		"SignName":         c.cfg.SignName,
		"TemplateId":       c.cfg.TemplateID,
		"TemplateParamSet": []string{code, "5"},
	})
	if err != nil {
		return smsProviderError("could not build request")
	}

	u, err := url.Parse(c.endpoint)
	if err != nil {
		return smsProviderError("could not parse endpoint")
	}
	timestamp := time.Now().Unix()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return smsProviderError("could not build request")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-TC-Action", actionName)
	req.Header.Set("X-TC-Version", apiVersion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Region", c.cfg.Region)
	req.Header.Set("Authorization", signTC3(c.cfg.SecretID, c.cfg.SecretKey, c.cfg.Region, serviceName, u.Host, timestamp, payload))

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return smsProviderError("request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return smsProviderError("could not read response")
	}
	return parseResponse(body)
}

// smsProviderError returns a 502 for any Tencent Cloud SMS failure (network,
// malformed response, or a non-Ok status). The spec treats upstream failures
// as a clear client-facing error the user can retry against.
func smsProviderError(detail string) error {
	return apperror.SmsProviderError(detail)
}

// --- TC3-HMAC-SHA256 signing ---

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(data))
	return m.Sum(nil)
}

// signTC3 builds the Authorization header for a Tencent Cloud TC3 request.
// Exported for tests so the expected signature can be recomputed from the
// captured timestamp and payload.
func signTC3(secretID, secretKey, region, service, host string, timestamp int64, payload []byte) string {
	payloadHash := sha256Hex(payload)
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", contentType, host)
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		"POST", "/", "", canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	hashedCanonical := sha256Hex([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(timestamp, 10),
		date + "/" + service + "/tc3_request",
		hashedCanonical,
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	credential := fmt.Sprintf("%s/%s/%s/tc3_request", secretID, date, service)
	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s, SignedHeaders=%s, Signature=%s",
		credential, signedHeaders, signature)
}

// --- Response parsing ---

type sendSmsResponse struct {
	Response struct {
		SendStatusSet []struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"SendStatusSet"`
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

func parseResponse(body []byte) error {
	var resp sendSmsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return smsProviderError("malformed response")
	}
	if resp.Response.Error != nil {
		return smsProviderError(resp.Response.Error.Message)
	}
	if len(resp.Response.SendStatusSet) == 0 {
		return smsProviderError("no send status in response")
	}
	st := resp.Response.SendStatusSet[0]
	if st.Code != "Ok" {
		return smsProviderError(st.Message)
	}
	return nil
}
