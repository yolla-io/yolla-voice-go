package yollavoice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	testKey    = "sk_live_supersecretkey"
	testSecret = "whsec_supersecretvalue"
)

func TestNewClientAPIKeyResolution(t *testing.T) {
	t.Setenv("YOLLA_API_KEY", "")
	_, err := NewClient()
	if err == nil || !strings.Contains(err.Error(), "WithAPIKey") || !strings.Contains(err.Error(), "YOLLA_API_KEY") {
		t.Fatalf("missing key error = %v", err)
	}

	t.Setenv("YOLLA_API_KEY", "sk_env")
	c, err := NewClient(WithAPIKey("sk_explicit"))
	if err != nil || c.Calls.t.apiKey != "sk_explicit" {
		t.Fatalf("explicit key: %v, %v", c, err)
	}
	c, err = NewClient(WithAPIKey(""))
	if err != nil || c.Calls.t.apiKey != "sk_env" {
		t.Fatalf("empty explicit key should fall back to env: %v", err)
	}
	c, err = NewClient()
	if err != nil || c.Calls.t.apiKey != "sk_env" {
		t.Fatalf("env key: %v", err)
	}
}

func TestNewClientOptionValidation(t *testing.T) {
	for name, o := range map[string]Option{
		"zero timeout":     WithTimeout(0),
		"negative timeout": WithTimeout(-time.Second),
		"negative retries": WithMaxRetries(-1),
		"empty secret":     WithWebhookSecret("ok", ""),
	} {
		if _, err := NewClient(WithAPIKey(testKey), o); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	c, err := NewClient(WithAPIKey(testKey), WithTimeout(time.Second), WithMaxRetries(0), nil)
	if err != nil || c.Calls.t.timeout != time.Second || c.Calls.t.maxRetries != 0 {
		t.Fatalf("valid options: %v", err)
	}
	c, _ = NewClient(WithAPIKey(testKey))
	if c.Calls.t.timeout != 30*time.Second || c.Calls.t.maxRetries != 2 {
		t.Fatalf("defaults: %v %v", c.Calls.t.timeout, c.Calls.t.maxRetries)
	}
	if c.Calls == nil || c.Balance == nil || c.Pricing == nil || c.Webhooks == nil {
		t.Fatal("services must be set")
	}
}

func TestWebhookSecrets(t *testing.T) {
	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	c, err := NewClient(WithAPIKey(testKey))
	if err != nil || len(c.Webhooks.secrets) != 0 {
		t.Fatalf("no secret must be fine: %v %v", c.Webhooks.secrets, err)
	}
	t.Setenv("YOLLA_WEBHOOK_SECRET", "whsec_env")
	c, _ = NewClient(WithAPIKey(testKey))
	if len(c.Webhooks.secrets) != 1 || c.Webhooks.secrets[0] != "whsec_env" {
		t.Fatalf("env secret: %v", c.Webhooks.secrets)
	}
	c, _ = NewClient(WithAPIKey(testKey), WithWebhookSecret("a", "b"))
	if len(c.Webhooks.secrets) != 2 || c.Webhooks.secrets[1] != "b" {
		t.Fatalf("explicit secrets: %v", c.Webhooks.secrets)
	}
}

func TestRedaction(t *testing.T) {
	c, err := NewClient(WithAPIKey(testKey), WithWebhookSecret(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.Webhooks.NewHandler(WebhookHandlerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := WebhookHandlerConfig{Secrets: []string{testSecret}}
	var outs []string
	for _, v := range []any{c, *c, c.Calls.t, *c.Calls.t, c.Calls, *c.Calls, c.Balance, *c.Balance,
		c.Pricing, *c.Pricing, c.Webhooks, *c.Webhooks, h, *h, cfg, &cfg} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%d", "%x", "%X", "%q", "%T %[1]v"} {
			outs = append(outs, fmt.Sprintf(f, v))
		}
		outs = append(outs, fmt.Sprint(v), fmt.Sprintln(v))
	}
	for _, v := range []any{c, *c, cfg, &cfg} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		outs = append(outs, string(b))
	}
	for _, o := range outs {
		if strings.Contains(o, testKey) || strings.Contains(o, testSecret) {
			t.Errorf("secret leaked: %s", o)
		}
	}
}

func TestRequestConfig(t *testing.T) {
	c, err := newRequestConfig(nil)
	if err != nil || c.timeout != nil || c.maxRetries != nil || c.idempotencyKey != "" {
		t.Fatalf("zero config: %+v %v", c, err)
	}
	c, err = newRequestConfig([]RequestOption{WithRequestTimeout(time.Second), WithRequestMaxRetries(0), WithIdempotencyKey("k"), nil})
	if err != nil || c.timeout == nil || *c.timeout != time.Second || c.maxRetries == nil || *c.maxRetries != 0 || c.idempotencyKey != "k" {
		t.Fatalf("config: %+v %v", c, err)
	}
	for _, o := range []RequestOption{WithRequestTimeout(-1), WithRequestTimeout(0), WithRequestMaxRetries(-1)} {
		if _, err := newRequestConfig([]RequestOption{o}); !errors.Is(err, ErrValidation) {
			t.Errorf("expected validation error, got %v", err)
		}
	}
}

func TestPtr(t *testing.T) {
	if p := Ptr(5); *p != 5 {
		t.Fatal("Ptr")
	}
}

func TestValidateNumber(t *testing.T) {
	for _, ok := range []string{"1415", "14155550100", "123456789012345"} {
		if err := validateNumber("to", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "141", "1234567890123456", "0155550100", "1415abc", "14 155", "١٢٣٤"} {
		err := validateNumber("to", bad)
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "to") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	err := validateNumber("to", "+14155550100")
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "drop the leading +") {
		t.Errorf("plus: %v", err)
	}
}

// testClient builds a client over rt with the transport internals replaced for offline tests.
func testClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	c, err := NewClient(WithAPIKey(testKey), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	sl := &sleeper{}
	c.Calls.t.sleep = sl.sleep
	return c
}
