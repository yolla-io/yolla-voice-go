package yollavoice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type pingResult struct {
	ID string `json:"id"`
}

func TestHTTPRequestShape(t *testing.T) {
	rt := script(respond(200, `{"id":"c1"}`))
	tr := newTestTransport(rt, &sleeper{})
	var out pingResult
	status, header, err := tr.do(context.Background(), request{
		method: http.MethodPost,
		path:   "/v1/calls/a%2Fb",
		query:  url.Values{"limit": {"10"}, "status": {"completed"}},
		body:   map[string]string{"to": "14155550100"},
		header: http.Header{"Idempotency-Key": {"key-1"}},
	}, &out)
	if err != nil || status != 200 || out.ID != "c1" || header.Get("Content-Type") != "application/json" {
		t.Fatalf("do: status %d, out %+v, err %v", status, out, err)
	}
	req := rt.requests[0]
	if got, want := req.URL.String(), "https://platform.yolla.io/api/v1/calls/a%2Fb?limit=10&status=completed"; got != want {
		t.Errorf("URL %q, want %q", got, want)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method %s", req.Method)
	}
	wantHeaders := map[string]string{
		"Authorization":   "Bearer sk_test_secret",
		"Accept":          "application/json",
		"User-Agent":      "yolla-voice-go/" + Version + " go/" + runtime.Version(),
		"Content-Type":    "application/json",
		"Idempotency-Key": "key-1",
	}
	for k, v := range wantHeaders {
		if got := req.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if rt.bodies[0] != `{"to":"14155550100"}` {
		t.Errorf("body %q", rt.bodies[0])
	}
	rt.assertClosed(t)
}

func TestHTTPNoBodyNoContentType(t *testing.T) {
	rt := script(respond(200, `{"id":"c1"}`))
	tr := newTestTransport(rt, &sleeper{})
	if _, _, err := tr.do(context.Background(), request{method: http.MethodGet, path: "/v1/balance"}, nil); err != nil {
		t.Fatal(err)
	}
	req := rt.requests[0]
	if req.Header.Get("Content-Type") != "" || rt.bodies[0] != "" {
		t.Errorf("GET without body: content type %q, body %q", req.Header.Get("Content-Type"), rt.bodies[0])
	}
	if req.URL.String() != "https://platform.yolla.io/api/v1/balance" {
		t.Errorf("URL %s", req.URL)
	}
}

func TestHTTPSuccessDecoding(t *testing.T) {
	cases := []struct {
		name   string
		status int
		ctype  string
		body   string
	}{
		{"200", 200, "application/json", `{"id":"x"}`},
		{"201", 201, "application/json", `{"id":"x"}`},
		{"charset", 200, "application/json; charset=utf-8", `{"id":"x"}`},
		{"problem+json", 200, "application/problem+json", `{"id":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := script(respond(c.status, c.body, "Content-Type", c.ctype))
			var out pingResult
			status, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, &out)
			if err != nil || status != c.status || out.ID != "x" {
				t.Errorf("status %d, out %+v, err %v", status, out, err)
			}
			rt.assertClosed(t)
		})
	}
}

func TestHTTPInvalidResponse(t *testing.T) {
	cases := []struct {
		name string
		step step
	}{
		{"empty", respond(200, "", "X-Request-Id", "req-9")},
		{"html", respond(200, "<html></html>", "Content-Type", "text/html", "X-Request-Id", "req-9")},
		{"malformed", respond(200, `{"id":`, "X-Request-Id", "req-9")},
		{"no content type", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": {"req-9"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"x"}`))}, nil
		}},
		{"too large", respond(200, `{"id":"`+strings.Repeat("a", maxBodySize)+`"}`, "X-Request-Id", "req-9")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := script(c.step)
			sl := &sleeper{}
			var out pingResult
			status, _, err := newTestTransport(rt, sl).do(context.Background(), request{method: "GET", path: "/x"}, &out)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err %v, want ErrInvalidResponse", err)
			}
			var ire *invalidResponseError
			if !errors.As(err, &ire) || ire.Status != 200 || ire.RequestID != "req-9" || status != 200 {
				t.Errorf("invalid response error %+v, status %d", ire, status)
			}
			if strings.Contains(err.Error(), "<html>") || strings.Contains(err.Error(), "aaaa") {
				t.Errorf("error leaks the body: %v", err)
			}
			if rt.calls() != 1 || sl.count() != 0 {
				t.Errorf("calls %d, sleeps %d: invalid responses are not retried", rt.calls(), sl.count())
			}
			rt.assertClosed(t)
		})
	}
}

func TestHTTPEmptyBodyWithoutOut(t *testing.T) {
	rt := script(respond(204, ""))
	status, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "POST", path: "/x"}, nil)
	if err != nil || status != 204 {
		t.Errorf("status %d, err %v", status, err)
	}
	rt.assertClosed(t)
}

func TestHTTPErrorBody(t *testing.T) {
	rt := script(respond(404, `{"error":{"code":"call_not_found","message":"Call not found","requestId":"req-1"}}`))
	_, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, &pingResult{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "call_not_found" || apiErr.RequestID != "req-1" || !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v", err)
	}
	// A JSON-shaped body with a non-JSON content type is not parsed.
	rt = script(respond(400, `{"error":{"code":"x","message":"y"}}`, "Content-Type", "text/plain", "X-Request-Id", "req-2"))
	_, _, err = newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, nil)
	if !errors.As(err, &apiErr) || apiErr.Code != "unknown_error" || apiErr.RequestID != "req-2" {
		t.Errorf("err %v", err)
	}
	rt.assertClosed(t)
}

func TestHTTPUnmarshalableBody(t *testing.T) {
	rt := script(respond(200, `{}`))
	_, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "POST", path: "/x", body: make(chan int)}, nil)
	if err == nil || rt.calls() != 0 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
}

func TestHTTPUserClientRedirect(t *testing.T) {
	rt := script(respond(302, "", "Location", "https://elsewhere.example/steal"), respond(200, `{"id":"x"}`))
	user := &http.Client{Transport: rt}
	tr := newTransport(user, "sk_test_secret", 0, 2)
	tr.sleep = (&sleeper{}).sleep
	_, _, err := tr.do(context.Background(), request{method: "GET", path: "/x"}, &pingResult{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 302 {
		t.Fatalf("err %v, want *APIError 302", err)
	}
	if rt.calls() != 1 {
		t.Errorf("calls %d: redirect must not be followed or retried", rt.calls())
	}
	if user.CheckRedirect != nil {
		t.Error("the user client was modified")
	}
	rt.assertClosed(t)
}

func TestHTTPDefaultClient(t *testing.T) {
	tr := newTransport(nil, "k", 0, 0)
	if tr.httpClient == nil || tr.httpClient == http.DefaultClient || tr.httpClient.CheckRedirect == nil {
		t.Fatal("default client must be a separate client that refuses redirects")
	}
	if http.DefaultClient.CheckRedirect != nil {
		t.Error("http.DefaultClient was modified")
	}
	if tr.httpClient.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Error("CheckRedirect must return http.ErrUseLastResponse")
	}
}

func TestHTTPConcurrent(t *testing.T) {
	rt := script(respond(200, `{"id":"x"}`))
	tr := newTestTransport(rt, &sleeper{})
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out pingResult
			if _, _, err := tr.do(context.Background(), request{method: "GET", path: "/x"}, &out); err != nil || out.ID != "x" {
				t.Errorf("err %v, out %+v", err, out)
			}
		}()
	}
	wg.Wait()
	if rt.calls() != 50 {
		t.Errorf("calls %d", rt.calls())
	}
	rt.assertClosed(t)
}
