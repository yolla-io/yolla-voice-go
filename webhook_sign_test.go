package yollavoice

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestSignWebhookRoundTrip(t *testing.T) {
	payload := map[string]any{"id": "evt_1", "type": "webhook.test", "seq": 0, "createdAt": "2026-11-23T10:15:00Z", "data": map[string]any{}}
	body, h, err := SignWebhook(payload, "whsec_test")
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("X-Voice-Timestamp") == "" || h.Get("X-Voice-Signature") == "" {
		t.Fatalf("headers %v", h)
	}
	ev, err := VerifyWebhook(body, h, []string{"whsec_test"})
	if err != nil || ev.ID() != "evt_1" {
		t.Fatalf("%v %v", ev, err)
	}
}

func TestSignWebhookDeterministic(t *testing.T) {
	ts := time.Unix(1792491675, 0)
	raw := []byte(`{"id":"evt_1","type":"x","data":{}}`)
	for _, payload := range []any{raw, json.RawMessage(raw)} {
		body, h, err := SignWebhook(payload, "whsec_test", WithSignTimestamp(ts))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != string(raw) {
			t.Fatalf("body %s", body)
		}
		if h.Get("X-Voice-Timestamp") != "1792491675" {
			t.Fatalf("ts %q", h.Get("X-Voice-Timestamp"))
		}
		if want := "v1=" + hmacHex("whsec_test", "1792491675", raw); h.Get("X-Voice-Signature") != want {
			t.Fatalf("sig %q want %q", h.Get("X-Voice-Signature"), want)
		}
	}
	// A struct payload is marshaled to JSON.
	body, _, err := SignWebhook(struct {
		A int `json:"a"`
	}{1}, "s", WithSignTimestamp(ts))
	if err != nil || string(body) != `{"a":1}` {
		t.Fatalf("%s %v", body, err)
	}
}

func TestSignWebhookErrors(t *testing.T) {
	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	if _, _, err := SignWebhook(map[string]any{}, ""); err == nil {
		t.Fatal("empty secret and no env must fail")
	}
	t.Setenv("YOLLA_WEBHOOK_SECRET", "envs")
	body, h, err := SignWebhook(map[string]any{"a": 1}, "")
	if err != nil || h.Get("X-Voice-Signature") != "v1="+hmacHex("envs", h.Get("X-Voice-Timestamp"), body) {
		t.Fatalf("env fallback: %v", err)
	}
	if _, _, err := SignWebhook(func() {}, "s"); err == nil {
		t.Fatal("unmarshalable payload must fail")
	}
}

func TestWebhooksServiceSignUsesFirstSecret(t *testing.T) {
	c, err := NewClient(WithAPIKey("k"), WithWebhookSecret("first", "second"))
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(5000, 0)
	body, h, err := c.Webhooks.Sign(json.RawMessage(`{"id":"e","type":"t"}`), WithSignTimestamp(ts))
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("X-Voice-Signature") != "v1="+hmacHex("first", strconv.Itoa(5000), body) {
		t.Fatal("must sign with the first secret")
	}
	var _ http.Header = h
}
