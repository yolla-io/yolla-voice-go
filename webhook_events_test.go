package yollavoice

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
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

func TestHoldConstantsMatchContract(t *testing.T) {
	data := contractSchema(t, "CallEvent")["allOf"].([]any)[1].(map[string]any)["properties"].(map[string]any)["data"]
	hold := data.(map[string]any)["properties"].(map[string]any)["hold"].(map[string]any)["properties"].(map[string]any)
	enum := func(name string) []string {
		var values []string
		for _, v := range hold[name].(map[string]any)["enum"].([]any) {
			values = append(values, v.(string))
		}
		slices.Sort(values)
		return values
	}
	cases := map[string][]string{
		"action":    {string(HoldActionHold), string(HoldActionUnhold)},
		"initiator": {string(HoldInitiatorClient), string(HoldInitiatorServer)},
		"leg":       {string(HoldLegCallee), string(HoldLegCaller)},
	}
	for name, got := range cases {
		if want := enum(name); !slices.Equal(got, want) {
			t.Errorf("hold %s constants = %v, contract = %v", name, got, want)
		}
	}
}

func TestOnlyPointerEventsImplementEvent(t *testing.T) {
	event := reflect.TypeFor[Event]()
	for _, v := range []any{CallEvent{}, QualityEvent{}, BalanceEvent{}, TestEvent{}, UnknownEvent{}} {
		typ := reflect.TypeOf(v)
		if typ.Implements(event) {
			t.Errorf("%s implements Event; only *%s must", typ, typ.Name())
		}
		if !reflect.PointerTo(typ).Implements(event) {
			t.Errorf("*%s does not implement Event", typ.Name())
		}
	}
}

func TestEventMarshalRoundTrip(t *testing.T) {
	f := loadWebhookFixture(t)
	bodies := append([]json.RawMessage{}, f.Events...)
	bodies = append(bodies,
		json.RawMessage(`{"id":"evt_u","type":"call.transferred","seq":4,"createdAt":"2026-11-23T10:15:00.25Z","data":{"to":"x","n":[1,2.50],"s":"é"}}`),
		json.RawMessage(`{"id":"evt_v","type":"call.transferred"}`),
	)
	for _, body := range bodies {
		ev, err := ParseWebhookEvent(body)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		out, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(out, &wire); err != nil {
			t.Fatal(err)
		}
		if string(wire["id"]) != strconv.Quote(ev.ID()) || string(wire["type"]) != strconv.Quote(ev.Type()) {
			t.Fatalf("wire shape lost the envelope: %s", out)
		}
		back, err := ParseWebhookEvent(out)
		if err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		if !reflect.DeepEqual(back, ev) {
			t.Fatalf("round trip changed the event:\n got %#v\nwant %#v", back, ev)
		}
	}
	u, _ := ParseWebhookEvent(bodies[len(bodies)-2])
	out, _ := json.Marshal(u)
	back, _ := ParseWebhookEvent(out)
	if got := string(back.(*UnknownEvent).Data); got != `{"to":"x","n":[1,2.50],"s":"é"}` {
		t.Fatalf("unknown data changed: %s", got)
	}
}

func TestParseUnknownEventLenientEnvelope(t *testing.T) {
	for _, body := range []string{
		`{"id":"e1","type":"call.transferred","seq":1.5,"data":{"a":1}}`,
		`{"id":"e1","type":"call.transferred","seq":"x","createdAt":"yesterday","data":{"a":1}}`,
	} {
		ev, err := ParseWebhookEvent([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		u, ok := ev.(*UnknownEvent)
		if !ok || u.ID() != "e1" || u.Seq() != 0 || !u.CreatedAt().IsZero() || string(u.Data) != `{"a":1}` {
			t.Fatalf("%s: %#v", body, ev)
		}
	}
	// Known types keep the strict envelope.
	for _, body := range []string{
		`{"id":"e1","type":"webhook.test","seq":1.5,"data":{}}`,
		`{"id":"e1","type":"webhook.test","createdAt":"yesterday","data":{}}`,
		`{"id":1,"type":"call.transferred"}`,
		`{"id":"e1","type":null}`,
	} {
		if _, err := ParseWebhookEvent([]byte(body)); err == nil {
			t.Fatalf("%s: want an error", body)
		}
	}
}
