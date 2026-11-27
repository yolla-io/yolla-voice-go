package yollavoice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const handlerSecret = "whsec_test"

// fixtureBody returns the fixture event body of the given type, and pins the clock.
func fixtureBody(t *testing.T, typ string) ([]byte, http.Header) {
	t.Helper()
	f := loadWebhookFixture(t)
	fixedNow(t, f.Timestamp)
	for _, raw := range f.Events {
		var e struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == typ {
			return []byte(raw), signedHeader(f.Secret, f.Timestamp, raw)
		}
	}
	t.Fatalf("no fixture event %q", typ)
	return nil, nil
}

func doPost(h http.Handler, body []byte, hdr http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	for k, v := range hdr {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wantResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if rec.Code != status || strings.TrimSpace(rec.Body.String()) != body {
		t.Fatalf("got %d %q, want %d %q", rec.Code, rec.Body.String(), status, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func newHandler(t *testing.T, cfg WebhookHandlerConfig) *WebhookHandler {
	t.Helper()
	if cfg.Secrets == nil {
		cfg.Secrets = []string{handlerSecret}
	}
	h, err := NewWebhookHandler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNewWebhookHandlerConfig(t *testing.T) {
	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	if _, err := NewWebhookHandler(WebhookHandlerConfig{}); err == nil || !strings.Contains(err.Error(), "YOLLA_WEBHOOK_SECRET") {
		t.Fatalf("want missing secret error, got %v", err)
	}
	if _, err := NewWebhookHandler(WebhookHandlerConfig{Secrets: []string{"s"}, MaxBodyBytes: -1}); err == nil {
		t.Fatal("negative MaxBodyBytes accepted")
	}
	if _, err := NewWebhookHandler(WebhookHandlerConfig{Secrets: []string{"s"}, Tolerance: -time.Second}); err == nil {
		t.Fatal("negative Tolerance accepted")
	}
	if _, err := NewWebhookHandler(WebhookHandlerConfig{Secrets: []string{"s"}, On: map[string]WebhookFunc{"future.type": nil}}); err != nil {
		t.Fatalf("unknown type key rejected: %v", err)
	}
	t.Setenv("YOLLA_WEBHOOK_SECRET", "from_env")
	if _, err := NewWebhookHandler(WebhookHandlerConfig{}); err != nil {
		t.Fatalf("env fallback: %v", err)
	}
}

func TestHandlerDispatch(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	type ctxKeyT struct{}
	ctxKey := ctxKeyT{}
	var gotCompleted, gotOther, gotUnhandled int
	var sawCtx bool
	h := newHandler(t, WebhookHandlerConfig{
		Secrets: []string{"old", "whsec_test"},
		On: map[string]WebhookFunc{
			EventCallCompleted: func(ctx context.Context, e Event) error {
				gotCompleted++
				sawCtx = ctx.Value(ctxKey) == "v"
				if _, ok := e.(*CallEvent); !ok {
					t.Errorf("event type %T", e)
				}
				return nil
			},
			EventCallExpired: func(context.Context, Event) error { gotOther++; return nil },
		},
		OnUnhandled: func(context.Context, Event) error { gotUnhandled++; return nil },
	})
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body)).WithContext(context.WithValue(context.Background(), ctxKey, "v"))
	req.Header = hdr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	wantResponse(t, rec, 200, `{"received":true}`)
	if gotCompleted != 1 || gotOther != 0 || gotUnhandled != 0 || !sawCtx {
		t.Fatalf("completed=%d other=%d unhandled=%d ctx=%v", gotCompleted, gotOther, gotUnhandled, sawCtx)
	}
}

func TestHandlerUnhandled(t *testing.T) {
	var types []string
	h := newHandler(t, WebhookHandlerConfig{OnUnhandled: func(_ context.Context, e Event) error {
		types = append(types, e.Type())
		return nil
	}})
	body, hdr := fixtureBody(t, EventCallCompleted)
	wantResponse(t, doPost(h, body, hdr), 200, `{"received":true}`)
	unknown := []byte(`{"id":"e1","type":"future.thing","seq":1,"createdAt":"2026-01-01T00:00:00Z","data":{"x":1}}`)
	wantResponse(t, doPost(h, unknown, signedHeader(handlerSecret, loadWebhookFixture(t).Timestamp, unknown)), 200, `{"received":true}`)
	if len(types) != 2 || types[1] != "future.thing" {
		t.Fatalf("types = %v", types)
	}
	nilUnhandled := newHandler(t, WebhookHandlerConfig{})
	wantResponse(t, doPost(nilUnhandled, body, hdr), 200, `{"received":true}`)
}

func TestHandlerBadSignature(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	hdr.Set("X-Voice-Signature", "v1=00")
	var got error
	h := newHandler(t, WebhookHandlerConfig{OnError: func(err error) { got = err }})
	wantResponse(t, doPost(h, body, hdr), 401, `{"error":"invalid signature"}`)
	wantReason(t, got, ReasonBadSignature)
}

func TestHandlerToleranceOption(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	ts := loadWebhookFixture(t).Timestamp
	fixedNow(t, ts+100)
	strict := newHandler(t, WebhookHandlerConfig{Tolerance: 10 * time.Second})
	wantResponse(t, doPost(strict, body, hdr), 401, `{"error":"invalid signature"}`)
	def := newHandler(t, WebhookHandlerConfig{})
	wantResponse(t, doPost(def, body, hdr), 200, `{"received":true}`)
}

func TestHandlerMethod(t *testing.T) {
	h := newHandler(t, WebhookHandlerConfig{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hook", nil))
	wantResponse(t, rec, 405, `{"error":"method not allowed"}`)
	if rec.Header().Get("Allow") != "POST" {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}
}

func TestHandlerBodyLimit(t *testing.T) {
	const mib = 1 << 20
	fixedNow(t, loadWebhookFixture(t).Timestamp)
	ts := loadWebhookFixture(t).Timestamp
	var seen int
	h := newHandler(t, WebhookHandlerConfig{OnError: func(error) { seen++ }})

	big := bytes.Repeat([]byte("a"), mib+1)
	wantResponse(t, doPost(h, big, signedHeader(handlerSecret, ts, big)), 413, `{"error":"payload too large"}`)

	// exactly 1 MiB is read (and then fails verification as invalid JSON payload, not 413)
	exact := bytes.Repeat([]byte("a"), mib)
	rec := doPost(h, exact, signedHeader(handlerSecret, ts, exact))
	if rec.Code == 413 {
		t.Fatal("1 MiB rejected")
	}

	small := newHandler(t, WebhookHandlerConfig{MaxBodyBytes: 10})
	wantResponse(t, doPost(small, []byte("0123456789a"), nil), 413, `{"error":"payload too large"}`)
	if rec := doPost(small, []byte("0123456789"), nil); rec.Code == 413 {
		t.Fatal("limit body rejected")
	}

	// chunked, no Content-Length
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, io.NopCloser(bytes.NewReader(big)))
	req.ContentLength = -1
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 413 || string(b) != `{"error":"payload too large"}` {
		t.Fatalf("chunked: %d %q", resp.StatusCode, b)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("boom secret-ish detail") }

func TestHandlerReadError(t *testing.T) {
	var got error
	h := newHandler(t, WebhookHandlerConfig{OnError: func(err error) { got = err }})
	req := httptest.NewRequest(http.MethodPost, "/hook", failingBody{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	wantResponse(t, rec, 500, `{"error":"handler failed"}`)
	if got == nil {
		t.Fatal("OnError not called")
	}
}

func TestHandlerCallbackError(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	sentinel := errors.New("db down: password=hunter2")
	var got error
	h := newHandler(t, WebhookHandlerConfig{
		On:      map[string]WebhookFunc{EventCallCompleted: func(context.Context, Event) error { return sentinel }},
		OnError: func(err error) { got = err },
	})
	rec := doPost(h, body, hdr)
	wantResponse(t, rec, 500, `{"error":"handler failed"}`)
	if !errors.Is(got, sentinel) {
		t.Fatalf("OnError got %v", got)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatal("internal error leaked")
	}
	// the same for OnUnhandled
	h2 := newHandler(t, WebhookHandlerConfig{OnUnhandled: func(context.Context, Event) error { return sentinel }})
	wantResponse(t, doPost(h2, body, hdr), 500, `{"error":"handler failed"}`)
}

func TestHandlerPanics(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	var got error
	var calls atomic.Int32
	h := newHandler(t, WebhookHandlerConfig{
		On: map[string]WebhookFunc{EventCallCompleted: func(context.Context, Event) error {
			if calls.Add(1) == 1 {
				panic("kaboom value")
			}
			return nil
		}},
		OnError: func(err error) { got = err; panic("onerror panics") },
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	post := func() (int, string) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
		req.Header = hdr.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if st, b := post(); st != 500 || b != `{"error":"handler failed"}` {
		t.Fatalf("panic: %d %q", st, b)
	}
	if got == nil || !strings.Contains(got.Error(), "kaboom value") {
		t.Fatalf("OnError got %v", got)
	}
	if strings.Contains(got.Error(), "goroutine") {
		t.Fatal("stack in error")
	}
	if st, b := post(); st != 200 || b != `{"received":true}` {
		t.Fatalf("next request: %d %q", st, b)
	}
}

func TestHandlerPanicWithError(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	sentinel := errors.New("inner")
	var got error
	h := newHandler(t, WebhookHandlerConfig{
		On:      map[string]WebhookFunc{EventCallCompleted: func(context.Context, Event) error { panic(sentinel) }},
		OnError: func(err error) { got = err },
	})
	wantResponse(t, doPost(h, body, hdr), 500, `{"error":"handler failed"}`)
	if !errors.Is(got, sentinel) {
		t.Fatalf("got %v", got)
	}
}

func TestHandlerParallel(t *testing.T) {
	body, hdr := fixtureBody(t, EventCallCompleted)
	var n atomic.Int64
	h := newHandler(t, WebhookHandlerConfig{On: map[string]WebhookFunc{
		EventCallCompleted: func(context.Context, Event) error { n.Add(1); return nil },
	}})
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := doPost(h, body, hdr.Clone()); rec.Code != 200 {
				t.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 32 {
		t.Fatalf("calls = %d", n.Load())
	}
}

func TestHandlerRedaction(t *testing.T) {
	h := newHandler(t, WebhookHandlerConfig{Secrets: []string{"super-secret-value"}})
	for _, s := range []string{fmt.Sprintf("%v", h), fmt.Sprintf("%+v", h), fmt.Sprintf("%#v", h), fmt.Sprintf("%v", *h)} {
		if strings.Contains(s, "super-secret-value") {
			t.Fatalf("secret leaked: %s", s)
		}
	}
}

func TestServiceNewHandler(t *testing.T) {
	t.Setenv("YOLLA_API_KEY", "k")
	t.Setenv("YOLLA_WEBHOOK_SECRET", "")
	c, err := NewClient(WithAPIKey("k"), WithWebhookSecret("whsec_test"))
	if err != nil {
		t.Fatal(err)
	}
	body, hdr := fixtureBody(t, EventCallCompleted)
	h, err := c.Webhooks.NewHandler(WebhookHandlerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, doPost(h, body, hdr), 200, `{"received":true}`)

	// explicit secrets win
	h2, err := c.Webhooks.NewHandler(WebhookHandlerConfig{Secrets: []string{"other"}})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, doPost(h2, body, hdr), 401, `{"error":"invalid signature"}`)

	c2, err := NewClient(WithAPIKey("k"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Webhooks.NewHandler(WebhookHandlerConfig{}); err == nil {
		t.Fatal("want error without secret")
	}
}
