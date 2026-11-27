package yollavoice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const (
	baseURL = "https://platform.yolla.io/api"
	// maxBodySize caps the response body the SDK reads.
	maxBodySize = 1 << 20
)

// transport sends API requests with authentication, retries and response decoding. It is safe for
// concurrent use.
type transport struct {
	httpClient *http.Client // a copy that never follows redirects
	apiKey     string
	timeout    time.Duration // per attempt; 0 or less means none
	maxRetries int
	userAgent  string

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	rand  func() float64 // in [0, 1]
}

// newTransport builds a transport. httpClient may be nil; a given client is copied, not modified.
func newTransport(httpClient *http.Client, apiKey string, timeout time.Duration, maxRetries int) *transport {
	var c http.Client
	if httpClient != nil {
		c = *httpClient
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &transport{
		httpClient: &c,
		apiKey:     apiKey,
		timeout:    timeout,
		maxRetries: maxRetries,
		userAgent:  "yolla-voice-go/" + Version + " go/" + runtime.Version(),
		now:        time.Now,
		sleep:      sleepContext,
		rand:       rand.Float64,
	}
}

// request is one API operation.
type request struct {
	method string
	path   string // already escaped, starts with "/"
	query  url.Values
	body   any         // JSON-encoded when non-nil
	header http.Header // extra headers, set after the defaults

	timeout    time.Duration // per-attempt override; 0 means the transport's
	maxRetries *int          // override; nil means the transport's
}

// attemptResult is the outcome of one attempt.
type attemptResult struct {
	status     int
	header     http.Header
	err        error
	retry      bool
	retryAfter time.Duration // from Retry-After, already capped; 0 if absent
}

// do sends req with retries and decodes a 2xx JSON body into out (when out is non-nil). It returns the
// status and headers of the last response, if any. Cancellation of ctx returns ctx.Err() unwrapped.
func (t *transport) do(ctx context.Context, req request, out any) (int, http.Header, error) {
	var body []byte
	if req.body != nil {
		b, err := json.Marshal(req.body)
		if err != nil {
			return 0, nil, err
		}
		body = b
	}
	target := baseURL + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	timeout := t.timeout
	if req.timeout != 0 {
		timeout = req.timeout
	}
	maxRetries := t.maxRetries
	if req.maxRetries != nil {
		maxRetries = *req.maxRetries
	}

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		r := t.attempt(ctx, req, target, body, timeout, out)
		if r.err == nil || !r.retry || attempt > maxRetries {
			return r.status, r.header, r.err
		}
		if err := t.sleep(ctx, backoff(attempt, r.retryAfter, t.rand)); err != nil {
			return 0, nil, err
		}
	}
}

func (t *transport) attempt(ctx context.Context, req request, target string, body []byte, timeout time.Duration, out any) attemptResult {
	var actx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		actx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		actx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	hreq, err := http.NewRequestWithContext(actx, req.method, target, reader)
	if err != nil {
		return attemptResult{err: err}
	}
	hreq.Header.Set("Authorization", "Bearer "+t.apiKey)
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("User-Agent", t.userAgent)
	if body != nil {
		hreq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.header {
		hreq.Header[http.CanonicalHeaderKey(k)] = v
	}

	resp, err := t.httpClient.Do(hreq)
	if err != nil {
		return t.failure(ctx, actx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return t.failure(ctx, actx, err)
	}
	r := attemptResult{status: resp.StatusCode, header: resp.Header}
	jsonBody := isJSON(resp.Header.Get("Content-Type"))

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		if out != nil {
			r.err = decode(resp, data, jsonBody, out)
		}
		return r
	}

	if !jsonBody {
		data = nil
	}
	apiErr := parseAPIError(resp.StatusCode, resp.Header, data, t.now())
	r.err = apiErr
	r.retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 && resp.StatusCode <= 599
	if r.retry {
		r.retryAfter = min(parseRetryAfter(resp.Header.Get("Retry-After"), t.now()), maxRetryAfter)
		if apiErr.RetryAfter > 0 {
			apiErr.RetryAfter = r.retryAfter
		}
	}
	return r
}

// failure classifies an attempt that got no complete response: caller cancellation (not retried), a
// per-attempt timeout or a connection error (both retried).
func (t *transport) failure(ctx, actx context.Context, err error) attemptResult {
	if ctx.Err() != nil {
		return attemptResult{err: ctx.Err()}
	}
	if errors.Is(actx.Err(), context.DeadlineExceeded) {
		return attemptResult{err: &timeoutError{}, retry: true}
	}
	return attemptResult{err: &ConnectionError{Err: err}, retry: true}
}

// decode parses a 2xx body into out.
func decode(resp *http.Response, data []byte, jsonBody bool, out any) error {
	var cause error
	switch {
	case len(data) > maxBodySize:
		cause = errors.New("response body exceeds 1 MiB")
	case !jsonBody:
		cause = errors.New("response is not JSON")
	case len(data) == 0:
		cause = errors.New("empty response body")
	default:
		cause = json.Unmarshal(data, out)
	}
	if cause == nil {
		return nil
	}
	return &invalidResponseError{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id"), Err: cause}
}

// isJSON reports whether a Content-Type is application/json or a +json type.
func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}
