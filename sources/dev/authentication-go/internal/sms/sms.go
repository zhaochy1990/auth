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
	"net/http"
	"net/url"
	"time"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/x/logger"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
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
	SecretID  string
	SecretKey string
	SDKAppID  string
	SignName  string
	// TemplateID is the login template (2716979), and the fallback for a scene
	// without a template of its own.
	TemplateID string
	// BindPhoneTemplateID is the 绑定手机号 template (2739819). Empty falls back
	// to TemplateID.
	BindPhoneTemplateID string
	// ResetPasswordTemplateID is the 找回密码 template (2716981). Empty falls
	// back to TemplateID.
	//
	// A scene's own template declares {1}=code alone, so it takes one param;
	// TemplateID's copy adds {2}=validity minutes and takes two. Give a scene a
	// template only if its copy has that single placeholder — Tencent rejects a
	// param count that does not match the template.
	ResetPasswordTemplateID string
	Region                  string
}

// Configured reports whether every credential needed to send is present.
func (c Config) Configured() bool {
	if c.SecretID == "" {
		logger.S().Warn("Tencent Cloud SMS SecretID is empty; /api/auth/sms/send will return sms_not_configured")
	}
	if c.SecretKey == "" {
		logger.S().Warn("Tencent Cloud SMS SecretKey is empty; /api/auth/sms/send will return sms_not_configured")
	}
	if c.SDKAppID == "" {
		logger.S().Warn("Tencent Cloud SMS SDKAppID is empty; /api/auth/sms/send will return sms_not_configured")
	}
	if c.SignName == "" {
		logger.S().Warn("Tencent Cloud SMS SignName is empty; /api/auth/sms/send will return sms_not_configured")
	}
	if c.TemplateID == "" {
		logger.S().Warn("Tencent Cloud SMS TemplateID is empty; /api/auth/sms/send will return sms_not_configured")
	}

	return c.SecretID != "" && c.SecretKey != "" && c.SDKAppID != "" && c.SignName != "" && c.TemplateID != ""
}

// Client calls the Tencent Cloud SMS API.
type Client struct {
	cfg        Config
	endpoint   string
	httpCli    *http.Client
	credential *common.Credential
}

// NewClient creates a Tencent SMS API client. endpoint overrides the default
// Tencent endpoint (used by tests to point at a fake server); pass "" for
// production. An unconfigured Config makes every call return a
// "not configured" error.
func NewClient(cfg Config, endpoint string) *Client {
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	if cfg.Configured() {
		cred := common.NewCredential(cfg.SecretID, cfg.SecretKey)
		return &Client{cfg: cfg, endpoint: endpoint, httpCli: &http.Client{Timeout: 10 * time.Second}, credential: cred}
	}

	return &Client{cfg: cfg, endpoint: endpoint, httpCli: &http.Client{Timeout: 10 * time.Second}}
}

// Configured reports whether the client has every credential needed to send.
func (c *Client) Configured() bool { return c.cfg.Configured() }

// SendCode delivers a verification code to a mainland-China phone number
// (bare 11 digits; the client prefixes +86). The scene selects the template
// (ADR 0010), so the message the user reads agrees with the action the code
// authorises: bind_phone and reset_password each have their own approved copy,
// and every other scene — including a scene this client does not know — sends
// the login template. Template params follow the template actually selected,
// since Tencent rejects a param count that does not match it.
func (c *Client) SendCode(ctx context.Context, scene repository.SmsScene, phone, code string) error {
	if !c.cfg.Configured() {
		return apperror.SmsNotConfigured()
	}

	// Route the SDK at the configured endpoint (the production default, or the
	// injected test server). The SDK builds the request from the profile's
	// scheme + host, so split the endpoint URL accordingly.
	cp := profile.NewClientProfile()
	if u, err := url.Parse(c.endpoint); err == nil && u.Host != "" {
		cp.HttpProfile.Scheme = u.Scheme
		cp.HttpProfile.Endpoint = u.Host
	} else {
		cp.HttpProfile.Endpoint = c.endpoint
	}
	client, _ := sms.NewClient(c.credential, c.cfg.Region, cp)

	// 实例化一个请求对象,每个接口都会对应一个request对象
	request := sms.NewSendSmsRequest()

	templateID, params := c.cfg.templateFor(scene, code)
	request.PhoneNumberSet = common.StringPtrs([]string{"+86" + phone})
	request.SmsSdkAppId = common.StringPtr(c.cfg.SDKAppID)
	request.TemplateId = common.StringPtr(templateID)
	request.SignName = common.StringPtr(c.cfg.SignName)
	request.TemplateParamSet = common.StringPtrs(params)
	// 返回的resp是一个SendSmsResponse的实例，与请求对象对应
	response, err := client.SendSms(request)
	if err != nil {
		// Network/transport failures and top-level API errors (Response.Error).
		if sdkErr, ok := err.(*errors.TencentCloudSDKError); ok {
			return smsProviderError(sdkErr.Message)
		}
		return smsProviderError("Tencent Cloud SMS request failed")
	}

	// A 200 response can still carry a per-phone business failure (Code != "Ok")
	// or an empty status set; treat both as a provider error.
	if response == nil || response.Response == nil || len(response.Response.SendStatusSet) == 0 {
		return smsProviderError("Tencent Cloud SMS returned no status")
	}
	for _, st := range response.Response.SendStatusSet {
		if st == nil || st.Code == nil || *st.Code != "Ok" {
			msg := "Tencent Cloud SMS send failure"
			if st != nil && st.Message != nil && *st.Message != "" {
				msg = *st.Message
			}
			return smsProviderError(msg)
		}
	}

	logger.S().Infof("Get response from tecent sms service %s", response.ToJsonString())
	return nil
}

// templateFor returns the approved Tencent Cloud template for a scene and its
// placeholder values. The scene's own template copy declares {1}=code alone;
// the login template adds {2}=validity minutes. A scene with no template of its
// own sends the login copy, so its params must be the login shape — the two are
// picked together because Tencent rejects a param count that does not match the
// template.
func (c Config) templateFor(scene repository.SmsScene, code string) (string, []string) {
	own := ""
	switch scene {
	case repository.SmsSceneBindPhone:
		own = c.BindPhoneTemplateID
	case repository.SmsSceneResetPassword:
		own = c.ResetPasswordTemplateID
	}
	if own != "" {
		return own, []string{code}
	}
	return c.TemplateID, []string{code, "5"}
}

// smsProviderError returns a 502 for any Tencent Cloud SMS failure (network,
// malformed response, or a non-Ok status). The spec treats upstream failures
// as a clear client-facing error the user can retry against.
func smsProviderError(detail string) error {
	return apperror.SmsProviderError(detail)
}
