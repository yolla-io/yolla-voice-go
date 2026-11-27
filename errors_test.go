package yollavoice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func TestAPIErrorSentinels(t *testing.T) {
	sentinels := []error{ErrValidation, ErrAuthentication, ErrInsufficientBalance, ErrPermission, ErrNotFound,
		ErrConflict, ErrRateLimit, ErrServer}
	cases := []struct {
		status int
		want   error
	}{
		{400, ErrValidation}, {422, ErrValidation}, {401, ErrAuthentication}, {402, ErrInsufficientBalance},
		{403, ErrPermission}, {404, ErrNotFound}, {409, ErrConflict}, {429, ErrRateLimit},
		{500, ErrServer}, {503, ErrServer}, {418, nil}, {302, nil},
	}
	for _, c := range cases {
		err := parseAPIError(c.status, http.Header{}, nil, testNow)
		if err.Status != c.status {
			t.Errorf("status %d: got %d", c.status, err.Status)
		}
		for _, s := range sentinels {
			if got, want := errors.Is(err, s), s == c.want; got != want {
				t.Errorf("status %d vs %v: Is=%v, want %v", c.status, s, got, want)
			}
		}
	}
	v := newValidationError("bad limit")
	if !errors.Is(v, ErrValidation) || v.Status != 0 || v.Code != "invalid_params" || v.Message != "bad limit" {
		t.Errorf("validation error: %+v", v)
	}
	if errors.Is(v, ErrServer) {
		t.Error("validation error matches ErrServer")
	}
}

func TestParseAPIErrorBody(t *testing.T) {
	body := []byte(`{"error":{"code":"destination_blocked","message":"blocked","requestId":"req_1"}}`)
	h := http.Header{"X-Request-Id": {"hdr"}}
	e := parseAPIError(403, h, body, testNow)
	if e.Code != "destination_blocked" || e.Message != "blocked" || e.RequestID != "req_1" {
		t.Errorf("got %+v", e)
	}
	e = parseAPIError(403, h, []byte(`{"error":{"code":"x","message":"m"}}`), testNow)
	if e.RequestID != "hdr" {
		t.Errorf("header fallback: %+v", e)
	}
	bad := []string{"", "not json", `{"error":"x"}`, `{"error":{"code":1,"message":"m"}}`,
		`{"error":{"code":"c"}}`, `{"error":{"code":"","message":"m"}}`, `[]`, `null`}
	for _, b := range bad {
		e := parseAPIError(502, h, []byte(b), testNow)
		if e.Code != "unknown_error" || !strings.Contains(e.Message, "502") {
			t.Errorf("body %q: %+v", b, e)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"3", 3 * time.Second},
		{" 7 ", 7 * time.Second},
		{testNow.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{testNow.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"soon", 0},
		{"-5", 0},
		{"", 0},
	}
	for _, c := range cases {
		h := http.Header{}
		if c.value != "" {
			h.Set("Retry-After", c.value)
		}
		if got := parseAPIError(429, h, nil, testNow).RetryAfter; got != c.want {
			t.Errorf("%q: got %v, want %v", c.value, got, c.want)
		}
	}
	h := http.Header{"Retry-After": {"3"}}
	if got := parseAPIError(503, h, nil, testNow).RetryAfter; got != 0 {
		t.Errorf("non-429 RetryAfter = %v", got)
	}
}

func TestParseRetryAfterPresence(t *testing.T) {
	for value, wantOK := range map[string]bool{
		"0": true, "3": true, testNow.Add(-time.Minute).Format(http.TimeFormat): true,
		"": false, "soon": false, "-5": false,
	} {
		if _, ok := parseRetryAfter(value, testNow); ok != wantOK {
			t.Errorf("%q: ok = %v, want %v", value, ok, wantOK)
		}
	}
}

func TestAPIErrorString(t *testing.T) {
	body := []byte(`{"unexpected":"sk_live_supersecret"}`)
	e := parseAPIError(500, http.Header{"X-Request-Id": {"req_9"}}, body, testNow)
	s := e.Error()
	for _, part := range []string{"500", "unknown_error", "req_9", "yollavoice: "} {
		if !strings.Contains(s, part) {
			t.Errorf("%q lacks %q", s, part)
		}
	}
	if strings.Contains(s, "supersecret") || strings.Contains(fmt.Sprintf("%+v %#v", e, e), "supersecret") {
		t.Error("body leaked")
	}
	e = parseAPIError(404, nil, []byte(`{"error":{"code":"call_not_found","message":"no such call"}}`), testNow)
	if s := e.Error(); !strings.Contains(s, "no such call") || !strings.Contains(s, "call_not_found") ||
		strings.Contains(s, "request") {
		t.Errorf("got %q", s)
	}
	if s := newValidationError("limit must be positive").Error(); !strings.Contains(s, "limit must be positive") {
		t.Errorf("got %q", s)
	}
}

func TestErrorsAsThroughWrapping(t *testing.T) {
	var target *APIError
	wrapped := fmt.Errorf("create call: %w", parseAPIError(404, nil, nil, testNow))
	if !errors.As(wrapped, &target) || target.Status != 404 || !errors.Is(wrapped, ErrNotFound) {
		t.Error("APIError not reachable")
	}
	cause := errors.New("dial tcp: refused")
	var conn *ConnectionError
	wrapped = fmt.Errorf("x: %w", &ConnectionError{Err: cause})
	if !errors.As(wrapped, &conn) || !errors.Is(wrapped, cause) {
		t.Error("ConnectionError not reachable or does not unwrap")
	}
	if !strings.Contains(conn.Error(), "dial tcp: refused") || !strings.HasPrefix(conn.Error(), "yollavoice: ") {
		t.Errorf("got %q", conn.Error())
	}
	if got := (&ConnectionError{}).Error(); got != "yollavoice: connection error" {
		t.Errorf("zero ConnectionError: %q", got)
	}
	urlErr := &url.Error{Op: "Get", URL: "https://platform.yolla.io/api/v1/calls?externalUserId=user-42", Err: cause}
	withURL := &ConnectionError{Err: urlErr}
	if got := withURL.Error(); got != "yollavoice: connection error: Get: dial tcp: refused" {
		t.Errorf("url error text: %q", got)
	}
	if !errors.Is(withURL, urlErr) {
		t.Error("Unwrap must keep the full url.Error")
	}
	for _, s := range []error{ErrServer, ErrTimeout, ErrInvalidResponse} {
		if errors.Is(conn, s) {
			t.Errorf("connection error matches %v", s)
		}
	}
}

func TestTimeoutError(t *testing.T) {
	err := fmt.Errorf("w: %w", &timeoutError{})
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("timeout error must match ErrTimeout and DeadlineExceeded")
	}
	if errors.Is(err, ErrServer) || errors.Is(err, ErrInvalidResponse) {
		t.Error("timeout error matches other sentinels")
	}
	if !strings.HasPrefix(err.Error(), "w: yollavoice: ") {
		t.Errorf("got %q", err.Error())
	}
}

func TestInvalidResponseError(t *testing.T) {
	cause := errors.New("unexpected end of JSON input")
	e := &invalidResponseError{Status: 200, RequestID: "req_2", Err: cause}
	if !errors.Is(e, ErrInvalidResponse) || !errors.Is(e, cause) || errors.Is(e, ErrTimeout) {
		t.Error("sentinel matching")
	}
	s := e.Error()
	if !strings.Contains(s, "200") || !strings.Contains(s, "req_2") || !strings.HasPrefix(s, "yollavoice: ") {
		t.Errorf("got %q", s)
	}
	if s := (&invalidResponseError{Status: 200}).Error(); strings.Contains(s, "request") {
		t.Errorf("got %q", s)
	}
}

func TestWebhookVerificationError(t *testing.T) {
	for _, r := range []string{ReasonMissingHeader, ReasonBadSignature, ReasonStaleTimestamp, ReasonInvalidPayload} {
		err := fmt.Errorf("w: %w", &WebhookVerificationError{Reason: r})
		var target *WebhookVerificationError
		if !errors.Is(err, ErrWebhookVerification) || !errors.As(err, &target) || target.Reason != r {
			t.Errorf("reason %s", r)
		}
		if !strings.Contains(err.Error(), r) {
			t.Errorf("%q lacks reason", err.Error())
		}
	}
	if ReasonMissingHeader != "missing_header" || ReasonBadSignature != "bad_signature" ||
		ReasonStaleTimestamp != "stale_timestamp" || ReasonInvalidPayload != "invalid_payload" {
		t.Error("reason values")
	}
	if errors.Is(&WebhookVerificationError{}, ErrServer) {
		t.Error("matches ErrServer")
	}
}
