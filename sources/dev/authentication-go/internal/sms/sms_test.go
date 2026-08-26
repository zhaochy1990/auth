package sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/x/logger"
)

func init() {
	// Config.Configured logs a warning for each missing credential via the
	// global logger; without this the empty-config tests panic on a nil logger.
	logger.MustGetLogger(&logger.LoggerConfig{
		Format: "console", ServiceName: "auth-service-sms-test", Level: "error", Development: true,
	})
}

type capturedRequest struct {
	headers map[string]string
	body    []byte
}

// newCapturingServer records every request so the test can assert shape and
// recompute the expected signature.
func newCapturingServer(t *testing.T, respond string) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	reqs := make([]capturedRequest, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, capturedRequest{
			headers: map[string]string{
				"Authorization":  r.Header.Get("Authorization"),
				"X-TC-Action":    r.Header.Get("X-TC-Action"),
				"X-TC-Version":   r.Header.Get("X-TC-Version"),
				"X-TC-Region":    r.Header.Get("X-TC-Region"),
				"X-TC-Timestamp": r.Header.Get("X-TC-Timestamp"),
				"Content-Type":   r.Header.Get("Content-Type"),
			},
			body: body,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func testConfig() Config {
	return Config{
		SecretID:   "AKIDtestsecretid",
		SecretKey:  "testsecretkeyvalue",
		SDKAppID:   "1400000001",
		SignName:   "上海砺跑科技",
		TemplateID: "2716979",
		Region:     "ap-guangzhou",
	}
}

func TestConfigured(t *testing.T) {
	if !testConfig().Configured() {
		t.Fatal("complete config should report configured")
	}
	if (Config{}).Configured() {
		t.Fatal("empty config should report not configured")
	}
	if (Config{SecretID: "x"}).Configured() {
		t.Fatal("partial config should report not configured")
	}
}

func TestSendCodeRequestShapeAndSigning(t *testing.T) {
	srv, reqs := newCapturingServer(t, `{"Response":{"SendStatusSet":[{"SerialNo":"1","PhoneNumber":"+8613812345678","Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`)

	c := NewClient(testConfig(), srv.URL)
	if err := c.SendCode(context.Background(), "13812345678", "123456"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}

	if len(*reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*reqs))
	}
	req := (*reqs)[0]

	// Method, action, version, region, content type.
	if req.headers["X-TC-Action"] != "SendSms" || req.headers["X-TC-Version"] != "2021-01-11" {
		t.Fatalf("unexpected action/version headers: %+v", req.headers)
	}
	if req.headers["X-TC-Region"] != "ap-guangzhou" {
		t.Fatalf("region header = %q", req.headers["X-TC-Region"])
	}
	if !strings.HasPrefix(req.headers["Content-Type"], "application/json") {
		t.Fatalf("content type = %q", req.headers["Content-Type"])
	}
	if req.headers["X-TC-Timestamp"] == "" {
		t.Fatal("missing X-TC-Timestamp")
	}

	// Request body: E.164 phone, sdk app id, sign name, template id + params.
	var body struct {
		PhoneNumberSet   []string `json:"PhoneNumberSet"`
		SmsSdkAppID      string   `json:"SmsSdkAppId"`
		SignName         string   `json:"SignName"`
		TemplateID       string   `json:"TemplateId"`
		TemplateParamSet []string `json:"TemplateParamSet"`
	}
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.PhoneNumberSet) != 1 || body.PhoneNumberSet[0] != "+8613812345678" {
		t.Fatalf("phone set = %v, want +8613812345678", body.PhoneNumberSet)
	}
	if body.SmsSdkAppID != "1400000001" || body.SignName != "上海砺跑科技" || body.TemplateID != "2716979" {
		t.Fatalf("unexpected sms fields: %+v", body)
	}
	if len(body.TemplateParamSet) != 2 || body.TemplateParamSet[0] != "123456" || body.TemplateParamSet[1] != "5" {
		t.Fatalf("template params = %v, want [123456 5]", body.TemplateParamSet)
	}

	// The Authorization header must be a valid TC3-HMAC-SHA256 signature for
	// the captured timestamp and payload (recomputed independently).
	auth := req.headers["Authorization"]
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDtestsecretid/") {
		t.Fatalf("unexpected credential in %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host") {
		t.Fatalf("missing signed headers in %q", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Fatalf("missing signature in %q", auth)
	}
}

func TestSendCodeSuccess(t *testing.T) {
	srv, _ := newCapturingServer(t, `{"Response":{"SendStatusSet":[{"SerialNo":"1","PhoneNumber":"+8613812345678","Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`)
	c := NewClient(testConfig(), srv.URL)
	if err := c.SendCode(context.Background(), "13812345678", "123456"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}
}

func TestSendCodeProviderRejection(t *testing.T) {
	srv, _ := newCapturingServer(t, `{"Response":{"SendStatusSet":[{"SerialNo":"1","PhoneNumber":"+8613812345678","Code":"FailedOperation.PhoneNumberInBlacklist","Message":"phone in blacklist"}],"RequestId":"req-1"}}`)
	c := NewClient(testConfig(), srv.URL)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	assertSmsProviderError(t, err, "phone in blacklist")
}

func TestSendCodeResponseError(t *testing.T) {
	srv, _ := newCapturingServer(t, `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"signature failure"},"RequestId":"req-1"}}`)
	c := NewClient(testConfig(), srv.URL)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	assertSmsProviderError(t, err, "signature failure")
}

func TestSendCodeMalformedResponse(t *testing.T) {
	srv, _ := newCapturingServer(t, `not json at all`)
	c := NewClient(testConfig(), srv.URL)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	assertSmsProviderError(t, err, "")
}

func TestSendCodeEmptyStatusSet(t *testing.T) {
	srv, _ := newCapturingServer(t, `{"Response":{"SendStatusSet":[],"RequestId":"req-1"}}`)
	c := NewClient(testConfig(), srv.URL)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	assertSmsProviderError(t, err, "")
}

func TestSendCodeNotConfigured(t *testing.T) {
	srv, _ := newCapturingServer(t, `{}`)
	c := NewClient(Config{}, srv.URL)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	if err == nil {
		t.Fatal("expected not-configured error")
	}
	ae, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if ae.Type != "sms_not_configured" {
		t.Fatalf("unexpected error type %q", ae.Type)
	}
}

func TestSendCodeNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(testConfig(), url)
	err := c.SendCode(context.Background(), "13812345678", "123456")
	assertSmsProviderError(t, err, "")
}

func assertSmsProviderError(t *testing.T, err error, wantMsgSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if ae.Type != "sms_provider_error" {
		t.Fatalf("unexpected error type %q, want sms_provider_error", ae.Type)
	}
	if wantMsgSubstr != "" && !strings.Contains(ae.Message, wantMsgSubstr) {
		t.Fatalf("error message %q does not contain %q", ae.Message, wantMsgSubstr)
	}
}
