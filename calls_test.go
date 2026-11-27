package yollavoice

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
)

const createdJSON = `{"id":"c1","status":"created","to":"37255512345","from":"37255500000","maxDuration":600,
"callToken":"tok","createdAt":"2026-11-27T10:00:00Z","expiresAt":"2026-11-27T10:01:00Z"}`

const callJSON = `{"id":"c1","status":"active","to":"37255512345","from":"37255500000","externalUserId":"u1",
"createdAt":"2026-11-27T10:00:00Z","expiresAt":"2026-11-27T10:01:00Z"}`

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validParams() CreateCallParams {
	return CreateCallParams{To: "37255512345", ExternalUserID: "u1"}
}

func TestCreateRequestAndReplayed(t *testing.T) {
	for status, replayed := range map[int]bool{201: false, 200: true} {
		rt := script(respond(status, createdJSON))
		c := testClient(t, rt)
		res, err := c.Calls.Create(context.Background(), validParams())
		if err != nil {
			t.Fatal(err)
		}
		if res.Replayed != replayed || res.Call.CallToken != "tok" || res.Call.ExpiresAt.IsZero() {
			t.Fatalf("status %d: %+v", status, res)
		}
		r := rt.requests[0]
		if r.Method != "POST" || r.URL.Path != "/api/v1/calls" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if k := r.Header.Get("Idempotency-Key"); !uuidV4.MatchString(k) {
			t.Fatalf("key %q", k)
		}
		if rt.bodies[0] != `{"to":"37255512345","externalUserId":"u1"}` {
			t.Fatalf("body %s", rt.bodies[0])
		}
	}
}

func TestCreateFullBody(t *testing.T) {
	rt := script(respond(201, createdJSON))
	c := testClient(t, rt)
	_, err := c.Calls.Create(context.Background(), CreateCallParams{
		To: "37255512345", From: Ptr("37255500000"), ExternalUserID: "u1",
		CustomParams: map[string]string{"k": "v"}, MaxDuration: Ptr(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(rt.bodies[0]), &m); err != nil || len(m) != 5 {
		t.Fatalf("body %s", rt.bodies[0])
	}
}

func TestCreateIdempotencyKeys(t *testing.T) {
	rt := script(respond(503, `{"error":{"code":"x","message":"m","requestId":"r"}}`), respond(201, createdJSON))
	c := testClient(t, rt)
	if _, err := c.Calls.Create(context.Background(), validParams()); err != nil {
		t.Fatal(err)
	}
	k1, k2 := rt.requests[0].Header.Get("Idempotency-Key"), rt.requests[1].Header.Get("Idempotency-Key")
	if rt.calls() != 2 || k1 == "" || k1 != k2 {
		t.Fatalf("retry keys %q %q", k1, k2)
	}
	if _, err := c.Calls.Create(context.Background(), validParams()); err != nil {
		t.Fatal(err)
	}
	if rt.requests[2].Header.Get("Idempotency-Key") == k1 {
		t.Fatal("separate creates must use different keys")
	}

	rt = script(respond(201, createdJSON))
	c = testClient(t, rt)
	if _, err := c.Calls.Create(context.Background(), validParams(), WithIdempotencyKey("order-A-1001")); err != nil {
		t.Fatal(err)
	}
	if rt.requests[0].Header.Get("Idempotency-Key") != "order-A-1001" {
		t.Fatal("custom key not sent as is")
	}
	if _, err := c.Calls.Create(context.Background(), validParams(), WithIdempotencyKey(strings.Repeat("k", 128))); err != nil {
		t.Fatal(err)
	}
	// The limit counts characters, not bytes: 128 two-byte characters pass.
	if _, err := c.Calls.Create(context.Background(), validParams(), WithIdempotencyKey(strings.Repeat("é", 128))); err != nil {
		t.Fatal(err)
	}
	n := rt.calls()
	_, err := c.Calls.Create(context.Background(), validParams(), WithIdempotencyKey(strings.Repeat("k", 129)))
	if !errors.Is(err, ErrValidation) || rt.calls() != n {
		t.Fatalf("129-char key: %v, calls %d", err, rt.calls())
	}
}

func TestCreateValidationBeforeIO(t *testing.T) {
	rt := script(respond(201, createdJSON))
	c := testClient(t, rt)
	ctx := context.Background()
	bad := []CreateCallParams{
		{To: "+37255512345", ExternalUserID: "u"},
		{To: "37255512345", From: Ptr("+37255500000"), ExternalUserID: "u"},
		{To: "123", ExternalUserID: "u"},
	}
	for _, p := range bad {
		if _, err := c.Calls.Create(ctx, p); !errors.Is(err, ErrValidation) {
			t.Errorf("%+v: %v", p, err)
		}
	}
	if _, err := c.Calls.Create(ctx, validParams(), WithRequestTimeout(-1)); !errors.Is(err, ErrValidation) {
		t.Errorf("bad option: %v", err)
	}
	if rt.calls() != 0 {
		t.Fatal("no I/O expected")
	}
	// An empty ExternalUserID is left to the API.
	if _, err := c.Calls.Create(ctx, CreateCallParams{To: "37255512345"}); err != nil || rt.calls() != 1 {
		t.Fatalf("empty external user id: %v", err)
	}
}

func TestCreateAPIError(t *testing.T) {
	rt := script(respond(402, `{"error":{"code":"insufficient_funds","message":"m","requestId":"r"}}`))
	c := testClient(t, rt)
	if _, err := c.Calls.Create(context.Background(), validParams()); err == nil {
		t.Fatal("expected API error")
	}
}

func TestCallActions(t *testing.T) {
	cases := []struct {
		name   string
		call   func(*Client, string, ...RequestOption) (*Call, error)
		method string
		suffix string
	}{
		{"get", func(c *Client, id string, o ...RequestOption) (*Call, error) {
			return c.Calls.Get(context.Background(), id, o...)
		}, "GET", ""},
		{"hold", func(c *Client, id string, o ...RequestOption) (*Call, error) {
			return c.Calls.Hold(context.Background(), id, o...)
		}, "POST", "/hold"},
		{"unhold", func(c *Client, id string, o ...RequestOption) (*Call, error) {
			return c.Calls.Unhold(context.Background(), id, o...)
		}, "POST", "/unhold"},
		{"end", func(c *Client, id string, o ...RequestOption) (*Call, error) {
			return c.Calls.End(context.Background(), id, o...)
		}, "POST", "/end"},
	}
	for _, tc := range cases {
		rt := script(respond(202, callJSON))
		c := testClient(t, rt)
		got, err := tc.call(c, "a/b c")
		if err != nil || got.ID != "c1" {
			t.Fatalf("%s: %v", tc.name, err)
		}
		r := rt.requests[0]
		if r.Method != tc.method || r.URL.EscapedPath() != "/api/v1/calls/a%2Fb%20c"+tc.suffix {
			t.Errorf("%s: %s %s", tc.name, r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("%s: unexpected idempotency key", tc.name)
		}
		if _, err := tc.call(c, ""); !errors.Is(err, ErrValidation) {
			t.Errorf("%s empty id: %v", tc.name, err)
		}
		if _, err := tc.call(c, "x", WithRequestMaxRetries(-1)); !errors.Is(err, ErrValidation) {
			t.Errorf("%s bad option: %v", tc.name, err)
		}
		if rt.calls() != 1 {
			t.Errorf("%s: validation must not do I/O", tc.name)
		}
		rt = script(respond(404, `{"error":{"code":"call_not_found","message":"m","requestId":"r"}}`))
		if _, err := tc.call(testClient(t, rt), "x"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s 404: %v", tc.name, err)
		}
	}
}

func TestHoldConflictAfterRetry(t *testing.T) {
	rt := script(respond(503, ""), respond(409, `{"error":{"code":"invalid_call_state","message":"m","requestId":"r"}}`))
	c := testClient(t, rt)
	_, err := c.Calls.Hold(context.Background(), "c1")
	var apiErr *APIError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &apiErr) || apiErr.Code != "invalid_call_state" || rt.calls() != 2 {
		t.Fatalf("err %v calls %d", err, rt.calls())
	}
}
