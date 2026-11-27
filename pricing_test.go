package yollavoice

import (
	"context"
	"errors"
	"testing"
)

const priceJSON = `{"number":"14155550100","country":"US","rate":{"prefix":"1415","perMinute":{"amount":"0.0100","currency":"USD"},"firstIncrementSec":60,"nextIncrementSec":6}}`

func TestGetNumberPrice(t *testing.T) {
	rt := script(respond(200, priceJSON, "Content-Type", "application/json"))
	c := testClient(t, rt)
	p, err := c.Pricing.GetNumberPrice(context.Background(), "14155550100")
	if err != nil {
		t.Fatal(err)
	}
	if p.Number != "14155550100" || p.Country == nil || *p.Country != "US" || p.Rate.Prefix != "1415" ||
		p.Rate.PerMinute.Amount != "0.0100" || p.Rate.FirstIncrementSec != 60 || p.Rate.NextIncrementSec != 6 {
		t.Fatalf("decoded: %+v", p)
	}
	r := rt.requests[0]
	if r.Method != "GET" || r.URL.EscapedPath() != "/api/v1/pricing/numbers/14155550100" {
		t.Fatalf("request: %s %s", r.Method, r.URL)
	}
}

func TestGetNumberPriceValidation(t *testing.T) {
	rt := script(respond(200, priceJSON, "Content-Type", "application/json"))
	c := testClient(t, rt)
	for _, n := range []string{"+14155550100", "1415abc", "", "../x"} {
		if _, err := c.Pricing.GetNumberPrice(context.Background(), n); !errors.Is(err, ErrValidation) {
			t.Errorf("%q: %v", n, err)
		}
	}
	if _, err := c.Pricing.GetNumberPrice(context.Background(), "14155550100", WithRequestMaxRetries(-1)); !errors.Is(err, ErrValidation) {
		t.Errorf("bad option: %v", err)
	}
	if rt.calls() != 0 {
		t.Fatalf("no request expected, got %d", rt.calls())
	}
}

func TestGetNumberPriceAPIError(t *testing.T) {
	rt := script(respond(404, `{"error":{"code":"not_found","message":"m","requestId":"r"}}`, "Content-Type", "application/json"))
	c := testClient(t, rt)
	if _, err := c.Pricing.GetNumberPrice(context.Background(), "14155550100"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
