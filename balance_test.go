package yollavoice

import (
	"context"
	"errors"
	"testing"
	"time"
)

const balanceJSON = `{"balance":{"amount":"12.50","currency":"USD"},"available":{"amount":"10.00","currency":"USD"},"creditLimit":{"amount":"5.00","currency":"USD"},"billingMode":"postpaid"}`

func TestBalanceGet(t *testing.T) {
	rt := script(respond(200, balanceJSON, "Content-Type", "application/json"))
	c := testClient(t, rt)
	b, err := c.Balance.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Balance.Amount != "12.50" || b.Available.Amount != "10.00" || b.Available.Currency != "USD" ||
		b.CreditLimit == nil || b.CreditLimit.Amount != "5.00" || b.BillingMode != BalanceBillingModePostpaid {
		t.Fatalf("decoded: %+v", b)
	}
	r := rt.requests[0]
	if r.Method != "GET" || r.URL.Path != "/api/v1/balance" || r.Header.Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("request: %s %s %v", r.Method, r.URL, r.Header)
	}
}

func TestBalanceGetErrorsAndOverrides(t *testing.T) {
	rt := script(respond(500, `{"error":{"code":"x","message":"m","requestId":"r"}}`, "Content-Type", "application/json"))
	c := testClient(t, rt)
	if _, err := c.Balance.Get(context.Background(), WithRequestMaxRetries(0)); !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v", err)
	}
	if rt.calls() != 1 {
		t.Fatalf("WithRequestMaxRetries(0) must give 1 attempt, got %d", rt.calls())
	}
	// The override is per call: the next call uses the client default (2 retries = 3 attempts).
	if _, err := c.Balance.Get(context.Background()); !errors.Is(err, ErrServer) {
		t.Fatal(err)
	}
	if rt.calls() != 4 {
		t.Fatalf("default retries: total attempts %d, want 4", rt.calls())
	}
	if _, err := c.Balance.Get(context.Background(), WithRequestTimeout(-1)); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad option: %v", err)
	}
}

func TestRequestTimeoutOverride(t *testing.T) {
	rt := script(block())
	c := testClient(t, rt)
	_, err := c.Balance.Get(context.Background(), WithRequestTimeout(10*time.Millisecond), WithRequestMaxRetries(0))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}
