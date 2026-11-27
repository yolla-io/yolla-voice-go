package yollavoice

import (
	"testing"
	"time"
)

func TestEventConstants(t *testing.T) {
	want := map[string]string{
		EventCallInitiated: "call.initiated", EventCallAnswered: "call.answered", EventCallHeld: "call.held",
		EventCallUnheld: "call.unheld", EventCallHoldFailed: "call.hold_failed", EventCallCompleted: "call.completed",
		EventCallExpired: "call.expired", EventCallQuality: "call.quality", EventBalanceLow: "balance.low",
		EventWebhookTest: "webhook.test",
	}
	for got, w := range want {
		if got != w {
			t.Fatalf("%q != %q", got, w)
		}
	}
}

func TestParseWebhookEvent(t *testing.T) {
	ev, err := ParseWebhookEvent([]byte(`{"id":"e1","type":"call.completed","seq":6,"createdAt":"2026-11-23T10:15:00.250Z","data":{"call":{"id":"call_1","status":"completed"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := ev.(*CallEvent)
	if !ok || c.Data.Call.ID != "call_1" || ev.ID() != "e1" || ev.Seq() != 6 ||
		!ev.CreatedAt().Equal(time.Date(2026, 11, 23, 10, 15, 0, 250e6, time.UTC)) {
		t.Fatalf("%#v", ev)
	}
	if _, err := ParseWebhookEvent([]byte(`{"type":"x"}`)); err == nil {
		t.Fatal("missing id must fail")
	}
}
