package yollavoice

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func page(ids string, next string) string {
	var items []string
	for _, id := range strings.Fields(ids) {
		items = append(items, `{"id":"`+id+`","status":"completed","to":"37255512345","from":"37255500000","externalUserId":"u1","createdAt":"2026-11-27T10:00:00Z","expiresAt":"2026-11-27T10:01:00Z"}`)
	}
	n := "null"
	if next != "" {
		n = `"` + next + `"`
	}
	return `{"items":[` + strings.Join(items, ",") + `],"nextCursor":` + n + `}`
}

func TestListQuery(t *testing.T) {
	rt := script(respond(200, page("a", "")))
	c := testClient(t, rt)
	_, err := c.Calls.List(context.Background(), ListCallsParams{})
	if err != nil {
		t.Fatal(err)
	}
	r := rt.requests[0]
	if r.Method != "GET" || r.URL.Path != "/api/v1/calls" || r.URL.RawQuery != "" {
		t.Fatalf("request %s %s", r.Method, r.URL)
	}

	zone := time.FixedZone("x", 2*3600)
	rt = script(respond(200, page("a", "")))
	c = testClient(t, rt)
	_, err = c.Calls.List(context.Background(), ListCallsParams{
		Status: CallStatusCompleted, ExternalUserID: "u 1", Limit: 25, Cursor: "cur",
		From: time.Date(2026, 11, 1, 10, 0, 0, 0, zone), To: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	q := rt.requests[0].URL.Query()
	want := url.Values{"status": {"completed"}, "externalUserId": {"u 1"}, "limit": {"25"}, "cursor": {"cur"},
		"from": {"2026-11-01T10:00:00+02:00"}, "to": {"2026-11-02T10:00:00Z"}}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("query %v", q)
	}
}

func TestListResponse(t *testing.T) {
	c := testClient(t, script(respond(200, page("a b", "n1"))))
	p, err := c.Calls.List(context.Background(), ListCallsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[1].ID != "b" || p.NextCursor != "n1" || !p.HasNextPage() {
		t.Fatalf("%+v", p)
	}
	c = testClient(t, script(respond(200, `{"items":[]}`)))
	p, err = c.Calls.List(context.Background(), ListCallsParams{})
	if err != nil || p.HasNextPage() || p.NextCursor != "" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestListErrors(t *testing.T) {
	c := testClient(t, script())
	if _, err := c.Calls.List(context.Background(), ListCallsParams{}, WithRequestTimeout(-1)); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := c.Calls.List(context.Background(), ListCallsParams{Limit: -1}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	c = testClient(t, script(respond(401, `{"error":{"code":"x","message":"m","requestId":"r"}}`)))
	if _, err := c.Calls.List(context.Background(), ListCallsParams{}); !errors.Is(err, ErrAuthentication) {
		t.Fatal(err)
	}
}

func TestNextPage(t *testing.T) {
	rt := script(respond(200, page("a", "n1")), respond(503, ""), respond(200, page("b", "")))
	c := testClient(t, rt)
	p, err := c.Calls.List(context.Background(), ListCallsParams{Status: CallStatusAnswered, Cursor: "first"}, WithRequestMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.NextPage(context.Background()); err == nil {
		t.Fatal("expected 503")
	}
	if rt.calls() != 2 {
		t.Fatalf("calls %d: retries option must carry over", rt.calls())
	}
	q := rt.requests[1].URL.Query()
	if q.Get("status") != "answered" || q.Get("cursor") != "n1" {
		t.Fatalf("query %v", q)
	}
	// the failed attempt leaves p usable
	p2, err := p.NextPage(context.Background())
	if err != nil || p2.HasNextPage() || p2.Items[0].ID != "b" {
		t.Fatalf("%+v %v", p2, err)
	}
	n := rt.calls()
	_, err = p2.NextPage(context.Background())
	if err == nil || rt.calls() != n {
		t.Fatalf("err %v calls %d", err, rt.calls())
	}
}

func collect(t *testing.T, c *Client, params ListCallsParams, stop int) (ids []string, err error) {
	t.Helper()
	for call, e := range c.Calls.ListAll(context.Background(), params) {
		if e != nil {
			if call.ID != "" {
				t.Fatalf("call with error: %+v", call)
			}
			return ids, e
		}
		ids = append(ids, call.ID)
		if stop > 0 && len(ids) == stop {
			break
		}
	}
	return ids, nil
}

func TestListAll(t *testing.T) {
	rt := script(respond(200, page("a b", "n1")), respond(200, page("c", "n2")), respond(200, page("d", "")))
	c := testClient(t, rt)
	seq := c.Calls.ListAll(context.Background(), ListCallsParams{Cursor: "start"})
	if rt.calls() != 0 {
		t.Fatal("request before iteration")
	}
	var ids []string
	for call, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, call.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b", "c", "d"}) || rt.calls() != 3 {
		t.Fatalf("%v calls %d", ids, rt.calls())
	}
	for i, want := range []string{"start", "n1", "n2"} {
		if got := rt.requests[i].URL.Query().Get("cursor"); got != want {
			t.Fatalf("request %d cursor %q", i, got)
		}
	}
}

func TestListAllBreakAndEmpty(t *testing.T) {
	rt := script(respond(200, page("a b", "n1")))
	ids, err := collect(t, testClient(t, rt), ListCallsParams{}, 1)
	if err != nil || len(ids) != 1 || rt.calls() != 1 {
		t.Fatalf("%v %v calls %d", ids, err, rt.calls())
	}
	rt = script(respond(200, page("", "")))
	ids, err = collect(t, testClient(t, rt), ListCallsParams{}, 0)
	if err != nil || len(ids) != 0 || rt.calls() != 1 {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestListAllErrors(t *testing.T) {
	rt := script(respond(200, page("a b", "n1")), respond(503, ""))
	c := testClient(t, rt)
	var got []string
	var errs int
	for call, err := range c.Calls.ListAll(context.Background(), ListCallsParams{}, WithRequestMaxRetries(0)) {
		if err != nil {
			errs++
			if call.ID != "" {
				t.Fatal("non-zero call with error")
			}
			continue
		}
		got = append(got, call.ID)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) || errs != 1 || rt.calls() != 2 {
		t.Fatalf("%v errs %d calls %d", got, errs, rt.calls())
	}
	// invalid first request yields one error
	ids, err := collect(t, testClient(t, script()), ListCallsParams{Limit: -1}, 0)
	if len(ids) != 0 || !errors.Is(err, ErrValidation) {
		t.Fatalf("%v %v", ids, err)
	}
}
