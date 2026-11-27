package yollavoice

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type webhookFixture struct {
	Secret     string `json:"secret"`
	Timestamp  int64  `json:"timestamp"`
	CrossCheck struct {
		Body      string `json:"body"`
		Signature string `json:"signature"`
	} `json:"crossCheck"`
	Events []json.RawMessage `json:"events"`
}

func loadWebhookFixture(t *testing.T) webhookFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/webhooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var f webhookFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// fixedNow pins the webhook clock to the fixture timestamp for the test.
func fixedNow(t *testing.T, unix int64) {
	t.Helper()
	old := webhookNow
	webhookNow = func() time.Time { return time.Unix(unix, 0) }
	t.Cleanup(func() { webhookNow = old })
}

func hmacHex(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func signedHeader(secret string, ts int64, body []byte) http.Header {
	h := http.Header{}
	s := strconv.FormatInt(ts, 10)
	h.Set("X-Voice-Timestamp", s)
	h.Set("X-Voice-Signature", "v1="+hmacHex(secret, s, body))
	return h
}

func wantReason(t *testing.T, err error, reason string) {
	t.Helper()
	if !errors.Is(err, ErrWebhookVerification) {
		t.Fatalf("want ErrWebhookVerification, got %v", err)
	}
	var we *WebhookVerificationError
	if !errors.As(err, &we) || we.Reason != reason {
		t.Fatalf("want reason %q, got %v", reason, err)
	}
}

func TestVerifyFixtures(t *testing.T) {
	f := loadWebhookFixture(t)
	fixedNow(t, f.Timestamp)
	wantTypes := []string{EventCallInitiated, EventCallAnswered, EventCallHeld, EventCallUnheld, EventCallHoldFailed,
		EventCallCompleted, EventCallBilled, EventCallExpired, EventCallQuality, EventBalanceLow, EventWebhookTest}
	if len(f.Events) != len(wantTypes) {
		t.Fatalf("fixture has %d events", len(f.Events))
	}
	for i, raw := range f.Events {
		body := []byte(raw)
		ev, err := VerifyWebhook(body, signedHeader(f.Secret, f.Timestamp, body), []string{f.Secret})
		if err != nil {
			t.Fatalf("%s: %v", wantTypes[i], err)
		}
		if ev.Type() != wantTypes[i] || !strings.HasPrefix(ev.ID(), "evt_") {
			t.Fatalf("%s: type %q id %q", wantTypes[i], ev.Type(), ev.ID())
		}
		if ev.CreatedAt().Year() != 2026 {
			t.Fatalf("%s: createdAt %v", wantTypes[i], ev.CreatedAt())
		}
		switch e := ev.(type) {
		case *CallEvent:
			if e.Data.Call.ID != "call_01JDPA0B1C2D3E4F5G6H7J8K9N" || e.Data.Call.ExternalUserID != "user-42" || ev.Seq() != int64(i+1) {
				t.Fatalf("%s: %+v seq %d", wantTypes[i], e.Data.Call, ev.Seq())
			}
			if (wantTypes[i] == EventCallHeld || wantTypes[i] == EventCallUnheld || wantTypes[i] == EventCallHoldFailed) != (e.Data.Hold != nil) {
				t.Fatalf("%s: hold %+v", wantTypes[i], e.Data.Hold)
			}
			if wantTypes[i] == EventCallHeld && (*e.Data.Hold.Action != HoldActionHold || *e.Data.Hold.Leg != HoldLegCaller) {
				t.Fatalf("hold %+v", e.Data.Hold)
			}
			if wantTypes[i] == EventCallBilled && (e.Data.Call.Price == nil || e.Data.Call.Price.Amount != "0.122" ||
				e.Data.Call.BilledDuration == nil || *e.Data.Call.BilledDuration != 61) {
				t.Fatalf("billed %+v", e.Data.Call)
			}
		case *QualityEvent:
			if e.Data.CallID == "" || e.Data.Quality.MOSEstimate == nil || *e.Data.Quality.MOSEstimate != 4.1 ||
				e.Data.Quality.Network == nil || len(e.Data.Quality.Warnings) != 1 {
				t.Fatalf("quality %+v", e.Data)
			}
		case *BalanceEvent:
			if e.Data.Available.Amount != "2.500000" || e.Data.BillingMode != BalanceBillingModePrepaid || e.Data.CreditLimit != nil {
				t.Fatalf("balance %+v", e.Data)
			}
		case *TestEvent:
			if ev.Seq() != 0 || e.Data == nil {
				t.Fatalf("test event %+v", e)
			}
		default:
			t.Fatalf("%s: unexpected type %T", wantTypes[i], ev)
		}
	}
}

func TestVerifyCrossCheckWithNodeVector(t *testing.T) {
	f := loadWebhookFixture(t)
	fixedNow(t, f.Timestamp)
	h := http.Header{}
	h.Set("X-Voice-Timestamp", strconv.FormatInt(f.Timestamp, 10))
	h.Set("X-Voice-Signature", f.CrossCheck.Signature)
	ev, err := VerifyWebhook([]byte(f.CrossCheck.Body), h, []string{f.Secret})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ev.(*TestEvent); !ok {
		t.Fatalf("got %T", ev)
	}
}

func TestVerifyUnknownType(t *testing.T) {
	fixedNow(t, 1000)
	body := []byte(`{"id":"evt_1","type":"call.transferred","seq":3,"createdAt":"2026-11-23T10:15:00Z","data":{"to":"x","n":[1,2]}}`)
	ev, err := VerifyWebhook(body, signedHeader("s", 1000, body), []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := ev.(*UnknownEvent)
	if !ok || string(u.Data) != `{"to":"x","n":[1,2]}` || u.Type() != "call.transferred" || u.Seq() != 3 {
		t.Fatalf("got %#v", ev)
	}
}

func TestVerifyHeaderReasons(t *testing.T) {
	fixedNow(t, 1000)
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	good := signedHeader("s", 1000, body)

	noTS := good.Clone()
	noTS.Del("X-Voice-Timestamp")
	noSig := good.Clone()
	noSig.Del("X-Voice-Signature")
	_, err := VerifyWebhook(body, noTS, []string{"s"})
	wantReason(t, err, ReasonMissingHeader)
	_, err = VerifyWebhook(body, noSig, []string{"s"})
	wantReason(t, err, ReasonMissingHeader)
	_, err = VerifyWebhook(body, nil, []string{"s"})
	wantReason(t, err, ReasonMissingHeader)

	// Header names in any case.
	lower := http.Header{"x-voice-timestamp": {"1000"}, "x-voice-signature": {good.Get("X-Voice-Signature")}}
	if _, err := VerifyWebhook(body, lower, []string{"s"}); err != nil {
		t.Fatalf("lowercase map keys: %v", err)
	}
	canon := http.Header{}
	canon.Set("x-VOICE-timestamp", "1000")
	canon.Set("X-voice-SIGNATURE", good.Get("X-Voice-Signature"))
	if _, err := VerifyWebhook(body, canon, []string{"s"}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBadSignature(t *testing.T) {
	fixedNow(t, 1000)
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	sig := hmacHex("s", "1000", body)
	set := func(v string) http.Header {
		h := http.Header{}
		h.Set("X-Voice-Timestamp", "1000")
		h.Set("X-Voice-Signature", v)
		return h
	}
	cases := map[string]struct {
		h    http.Header
		body []byte
		sec  string
	}{
		"wrong secret": {set("v1=" + sig), body, "other"},
		"v0 prefix":    {set("v0=" + sig), body, "s"},
		"no prefix":    {set(sig), body, "s"},
		"uppercase":    {set("v1=" + strings.ToUpper(sig)), body, "s"},
		"short":        {set("v1=" + sig[:10]), body, "s"},
		"body changed": {set("v1=" + sig), append(append([]byte{}, body[:len(body)-1]...), ' '), "s"},
	}
	for name, c := range cases {
		_, err := VerifyWebhook(c.body, c.h, []string{c.sec})
		wantReason(t, err, ReasonBadSignature)
		if strings.Contains(err.Error(), sig) || strings.Contains(err.Error(), c.sec+"\"") {
			t.Fatalf("%s: error leaks: %v", name, err)
		}
	}
}

func TestVerifyTimestamp(t *testing.T) {
	fixedNow(t, 10000)
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	for ts, ok := range map[int64]bool{9700: true, 9699: false, 10300: true, 10301: false} {
		_, err := VerifyWebhook(body, signedHeader("s", ts, body), []string{"s"})
		if ok && err != nil {
			t.Fatalf("ts %d: %v", ts, err)
		}
		if !ok {
			wantReason(t, err, ReasonStaleTimestamp)
		}
	}
	h := signedHeader("s", 10000, body)
	// Non-digit timestamps are malformed headers, as in Node.
	for _, bad := range []string{"abc", "-5", "1.5", "+1", "+1000", "1e3", "0x10"} {
		h.Set("X-Voice-Timestamp", bad)
		_, err := VerifyWebhook(body, h, []string{"s"})
		wantReason(t, err, ReasonMissingHeader)
	}
	// A digit string beyond int64 is a number far from now.
	h.Set("X-Voice-Timestamp", "99999999999999999999")
	_, err := VerifyWebhook(body, h, []string{"s"})
	wantReason(t, err, ReasonStaleTimestamp)
	// 9 minutes old passes only with a larger tolerance.
	old := signedHeader("s", 10000-540, body)
	_, err = VerifyWebhook(body, old, []string{"s"})
	wantReason(t, err, ReasonStaleTimestamp)
	if _, err := VerifyWebhook(body, old, []string{"s"}, WithTolerance(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyInvalidPayload(t *testing.T) {
	fixedNow(t, 1000)
	for _, raw := range []string{`not json`, `[]`, `[{"id":"a","type":"b"}]`, `null`, `{"type":"x"}`, `{"id":"x"}`,
		`{"id":1,"type":"x"}`, `{"id":"a","type":"call.completed","seq":"x"}`, `{"id":"a","type":"call.completed","seq":1,"createdAt":"nope","data":{}}`,
		`{"id":"a","type":"call.completed","seq":1,"createdAt":"2026-11-23T10:15:00Z","data":[]}`} {
		body := []byte(raw)
		_, err := VerifyWebhook(body, signedHeader("s", 1000, body), []string{"s"})
		wantReason(t, err, ReasonInvalidPayload)
	}
}

func TestVerifyRotation(t *testing.T) {
	fixedNow(t, 1000)
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	for _, signer := range []string{"new", "old"} {
		if _, err := VerifyWebhook(body, signedHeader(signer, 1000, body), []string{"new", "old"}); err != nil {
			t.Fatalf("%s: %v", signer, err)
		}
	}
}

func TestVerifySecrets(t *testing.T) {
	fixedNow(t, 1000)
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	h := signedHeader("envsecret", 1000, body)

	t.Setenv("YOLLA_WEBHOOK_SECRET", "envsecret")
	if _, err := VerifyWebhook(body, h, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyWebhook(body, h, []string{""}); err != nil {
		t.Fatalf("single empty secret falls back to env: %v", err)
	}
	_, err := VerifyWebhook(body, h, []string{"a", ""})
	if err == nil || errors.Is(err, ErrWebhookVerification) {
		t.Fatalf("empty secret in list: %v", err)
	}

	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	_, err = VerifyWebhook(body, h, nil)
	if err == nil || errors.Is(err, ErrWebhookVerification) || !strings.Contains(err.Error(), "YOLLA_WEBHOOK_SECRET") {
		t.Fatalf("no secret: %v", err)
	}
	var we *WebhookVerificationError
	if errors.As(err, &we) {
		t.Fatal("must not be a WebhookVerificationError")
	}
}

func TestResolveSecrets(t *testing.T) {
	t.Setenv("YOLLA_WEBHOOK_SECRET", "e")
	got, err := resolveSecrets([]string{"a", "b"})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	got, err = resolveSecrets(nil)
	if err != nil || len(got) != 1 || got[0] != "e" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestWebhooksServiceVerify(t *testing.T) {
	fixedNow(t, 1000)
	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	body := []byte(`{"id":"e","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`)
	c, err := NewClient(WithAPIKey("k"), WithWebhookSecret("cs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Webhooks.Verify(body, signedHeader("cs", 1000, body)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Webhooks.Verify(body, signedHeader("cs", 1000, body), WithTolerance(time.Second)); err != nil {
		t.Fatal(err)
	}
	nc, err := NewClient(WithAPIKey("k"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nc.Webhooks.Verify(body, signedHeader("cs", 1000, body)); err == nil || errors.Is(err, ErrWebhookVerification) {
		t.Fatalf("no client secret: %v", err)
	}
	if _, _, err := nc.Webhooks.Sign(map[string]any{}); err == nil {
		t.Fatal("Sign without secret must fail")
	}
}
