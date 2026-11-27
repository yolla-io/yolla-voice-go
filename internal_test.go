package yollavoice

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// step answers one request of a scriptedTransport.
type step func(req *http.Request) (*http.Response, error)

// scriptedTransport is an http.RoundTripper that answers requests with scripted steps. When the
// script runs out, the last step repeats. It records requests and counts opened and closed bodies.
type scriptedTransport struct {
	mu       sync.Mutex
	steps    []step
	requests []*http.Request
	bodies   []string
	opened   int
	closed   int
}

func script(steps ...step) *scriptedTransport { return &scriptedTransport{steps: steps} }

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	i := len(s.requests)
	s.requests = append(s.requests, req)
	var body string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		body = string(b)
	}
	s.bodies = append(s.bodies, body)
	st := s.steps[min(i, len(s.steps)-1)]
	s.mu.Unlock()

	resp, err := st(req)
	if resp != nil {
		s.mu.Lock()
		s.opened++
		s.mu.Unlock()
		resp.Body = &countingBody{Reader: resp.Body, close: func() {
			s.mu.Lock()
			s.closed++
			s.mu.Unlock()
		}}
		resp.Request = req
	}
	return resp, err
}

func (s *scriptedTransport) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// assertClosed fails the test unless every response body was closed exactly once.
func (s *scriptedTransport) assertClosed(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened != s.closed {
		t.Errorf("response bodies: opened %d, closed %d", s.opened, s.closed)
	}
}

type countingBody struct {
	io.Reader
	once  sync.Once
	close func()
}

func (b *countingBody) Close() error {
	b.once.Do(b.close)
	return nil
}

// respond returns a step answering with the status, JSON body and extra header pairs.
func respond(status int, body string, headerPairs ...string) step {
	return func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		if body != "" {
			h.Set("Content-Type", "application/json")
		}
		for i := 0; i+1 < len(headerPairs); i += 2 {
			h.Set(headerPairs[i], headerPairs[i+1])
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

// fail returns a step that fails with a transport error.
func fail(err error) step {
	return func(*http.Request) (*http.Response, error) { return nil, err }
}

// block returns a step that waits until the request context is done.
func block() step {
	return func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
}

// sleeper records requested sleeps without sleeping. hook, when set, runs instead of returning nil.
type sleeper struct {
	mu     sync.Mutex
	delays []time.Duration
	hook   func(ctx context.Context) error
}

func (s *sleeper) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.delays = append(s.delays, d)
	hook := s.hook
	s.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	return nil
}

func (s *sleeper) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.delays)
}

// newTestTransport builds a transport over rt with a fixed clock, recorded sleeps and rand fixed at 1.0.
func newTestTransport(rt http.RoundTripper, sl *sleeper) *transport {
	t := newTransport(&http.Client{Transport: rt}, "sk_test_secret", 30*time.Second, 2)
	t.now = func() time.Time { return testNow }
	t.sleep = sl.sleep
	t.rand = func() float64 { return 1.0 }
	return t
}
