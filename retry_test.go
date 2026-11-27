package yollavoice

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryServerErrorThenSuccess(t *testing.T) {
	rt := script(respond(503, `{}`), respond(503, `{}`), respond(200, `{"id":"x"}`))
	sl := &sleeper{}
	var out pingResult
	status, _, err := newTestTransport(rt, sl).do(context.Background(), request{method: "GET", path: "/x"}, &out)
	if err != nil || status != 200 || out.ID != "x" {
		t.Fatalf("status %d, err %v", status, err)
	}
	if rt.calls() != 3 || sl.count() != 2 {
		t.Errorf("calls %d, sleeps %d", rt.calls(), sl.count())
	}
	if sl.delays[0] != 500*time.Millisecond || sl.delays[1] != time.Second {
		t.Errorf("delays %v", sl.delays)
	}
	rt.assertClosed(t)
}

func TestRetryExhausted(t *testing.T) {
	rt := script(
		respond(500, `{"error":{"code":"internal","message":"m","requestId":"req-1"}}`),
		respond(500, `{"error":{"code":"internal","message":"m","requestId":"req-2"}}`),
		respond(500, `{"error":{"code":"internal","message":"m","requestId":"req-3"}}`),
		respond(500, `{"error":{"code":"internal","message":"m","requestId":"req-4"}}`),
	)
	status, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RequestID != "req-3" || !errors.Is(err, ErrServer) || status != 500 {
		t.Fatalf("err %v, status %d", err, status)
	}
	if rt.calls() != 3 {
		t.Errorf("calls %d, want 3", rt.calls())
	}
	rt.assertClosed(t)
}

func TestRetryNotForClientErrors(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 409, 422} {
		rt := script(respond(status, `{}`), respond(200, `{}`))
		sl := &sleeper{}
		_, _, err := newTestTransport(rt, sl).do(context.Background(), request{method: "GET", path: "/x"}, nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != status || rt.calls() != 1 || sl.count() != 0 {
			t.Errorf("status %d: err %v, calls %d", status, err, rt.calls())
		}
		rt.assertClosed(t)
	}
}

func TestRetryRedirectNotFollowed(t *testing.T) {
	rt := script(respond(302, "", "Location", "https://elsewhere.example/x"), respond(200, `{"id":"x"}`))
	_, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, &pingResult{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 302 {
		t.Fatalf("err %v", err)
	}
	if rt.calls() != 1 || rt.requests[0].URL.Host != "platform.yolla.io" {
		t.Errorf("calls %d: Location must not be requested", rt.calls())
	}
	rt.assertClosed(t)
}

func TestRetryTransportError(t *testing.T) {
	cause := errors.New("connection reset")
	rt := script(fail(cause), respond(200, `{"id":"x"}`))
	var out pingResult
	if _, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, &out); err != nil || out.ID != "x" {
		t.Fatalf("err %v", err)
	}

	last := errors.New("connection refused")
	rt = script(fail(cause), fail(cause), fail(last))
	_, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, nil)
	var connErr *ConnectionError
	if !errors.As(err, &connErr) || connErr.Err == nil || !errors.Is(err, last) {
		t.Fatalf("err %v, want *ConnectionError wrapping the last cause", err)
	}
	if rt.calls() != 3 {
		t.Errorf("calls %d", rt.calls())
	}
}

func TestRetryPerRequestMaxRetries(t *testing.T) {
	rt := script(respond(503, `{}`))
	zero := 0
	_, _, err := newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x", maxRetries: &zero}, nil)
	if !errors.Is(err, ErrServer) || rt.calls() != 1 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
	five := 5
	rt = script(respond(503, `{}`))
	_, _, _ = newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x", maxRetries: &five}, nil)
	if rt.calls() != 6 {
		t.Errorf("calls %d, want 6", rt.calls())
	}
}

func TestRetryAfterHeader(t *testing.T) {
	cases := []struct {
		name   string
		status int
		value  string
		want   time.Duration
		errRA  time.Duration
	}{
		{"seconds", 429, "2", 2 * time.Second, 2 * time.Second},
		{"capped", 429, "120", 60 * time.Second, 60 * time.Second},
		{"date", 429, testNow.Add(5 * time.Second).Format(http.TimeFormat), 5 * time.Second, 5 * time.Second},
		{"past date falls back to backoff", 429, testNow.Add(-time.Hour).Format(http.TimeFormat), 500 * time.Millisecond, 0},
		{"garbage", 429, "soon", 500 * time.Millisecond, 0},
		{"on 503", 503, "3", 3 * time.Second, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := script(respond(c.status, `{}`, "Retry-After", c.value))
			sl := &sleeper{}
			zero := 1
			_, _, err := newTestTransport(rt, sl).do(context.Background(), request{method: "GET", path: "/x", maxRetries: &zero}, nil)
			if sl.count() != 1 || sl.delays[0] != c.want {
				t.Errorf("delays %v, want %v", sl.delays, c.want)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.RetryAfter != c.errRA {
				t.Errorf("err %v RetryAfter, want %v", err, c.errRA)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	one := func() float64 { return 1.0 }
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := backoff(i+1, 0, one); got != w {
			t.Errorf("attempt %d: %v, want %v", i+1, got, w)
		}
	}
	if got := backoff(100, 0, one); got != 8*time.Second {
		t.Errorf("large attempt: %v", got)
	}
	if got := backoff(3, 0, func() float64 { return 0 }); got != 0 {
		t.Errorf("rand 0: %v", got)
	}
	if got := backoff(5, 2*time.Second, one); got != 2*time.Second {
		t.Errorf("retry-after: %v", got)
	}
	if got := backoff(1, 120*time.Second, one); got != 60*time.Second {
		t.Errorf("retry-after cap: %v", got)
	}
}

func TestRetryContextCancelledDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := script(respond(503, `{}`), respond(200, `{}`))
	sl := &sleeper{hook: func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	}}
	_, _, err := newTestTransport(rt, sl).do(ctx, request{method: "GET", path: "/x"}, nil)
	if err != context.Canceled {
		t.Fatalf("err %v, want context.Canceled unwrapped", err)
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) || rt.calls() != 1 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
	rt.assertClosed(t)
}

func TestRetryExpiredContext(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	rt := script(respond(200, `{}`))
	_, _, err := newTestTransport(rt, &sleeper{}).do(ctx, request{method: "GET", path: "/x"}, nil)
	if err != context.DeadlineExceeded || rt.calls() != 0 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
}

func TestRetryCallerCancelledDuringAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := script(func(req *http.Request) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	sl := &sleeper{}
	_, _, err := newTestTransport(rt, sl).do(ctx, request{method: "GET", path: "/x"}, nil)
	if err != context.Canceled || rt.calls() != 1 || sl.count() != 0 {
		t.Errorf("err %v, calls %d, sleeps %d", err, rt.calls(), sl.count())
	}
}

func TestRetryPerAttemptTimeout(t *testing.T) {
	rt := script(block(), respond(200, `{"id":"x"}`))
	tr := newTestTransport(rt, &sleeper{})
	tr.timeout = time.Nanosecond
	var out pingResult
	_, _, err := tr.do(context.Background(), request{method: "GET", path: "/x"}, &out)
	if err != nil || rt.calls() != 2 {
		t.Fatalf("err %v, calls %d", err, rt.calls())
	}

	rt = script(block())
	sl := &sleeper{}
	tr = newTestTransport(rt, sl)
	_, _, err = tr.do(context.Background(), request{method: "GET", path: "/x", timeout: time.Nanosecond}, nil)
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v, want ErrTimeout", err)
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) || rt.calls() != 3 || sl.count() != 2 {
		t.Errorf("err %v, calls %d, sleeps %d", err, rt.calls(), sl.count())
	}
}

func TestRetryTimeoutWhileReadingBody(t *testing.T) {
	rt := script(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: &ctxReader{ctx: req.Context()}}, nil
	})
	tr := newTestTransport(rt, &sleeper{})
	_, _, err := tr.do(context.Background(), request{method: "GET", path: "/x", timeout: time.Nanosecond}, &pingResult{})
	if !errors.Is(err, ErrTimeout) || rt.calls() != 3 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
	rt.assertClosed(t)

	rt = script(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &ctxReader{err: errors.New("reset")}}, nil
	})
	_, _, err = newTestTransport(rt, &sleeper{}).do(context.Background(), request{method: "GET", path: "/x"}, nil)
	var connErr *ConnectionError
	if !errors.As(err, &connErr) || rt.calls() != 3 {
		t.Errorf("err %v, calls %d", err, rt.calls())
	}
	rt.assertClosed(t)
}

// ctxReader fails with err, or waits for ctx and fails with its error.
type ctxReader struct {
	ctx context.Context
	err error
}

func (r *ctxReader) Read([]byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *ctxReader) Close() error { return nil }

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Nanosecond); err != nil {
		t.Errorf("short sleep: %v", err)
	}
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Errorf("zero sleep: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); err != context.Canceled {
		t.Errorf("cancelled sleep: %v", err)
	}
	if err := sleepContext(ctx, 0); err != context.Canceled {
		t.Errorf("cancelled zero sleep: %v", err)
	}
}
