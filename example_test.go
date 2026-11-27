package yollavoice_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

// roundTripFunc answers requests without a network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func exampleClient(handle roundTripFunc) *yollavoice.Client {
	client, err := yollavoice.NewClient(
		yollavoice.WithAPIKey("example-api-key"),
		yollavoice.WithHTTPClient(&http.Client{Transport: handle}),
	)
	if err != nil {
		panic(err)
	}
	return client
}

func ExampleNewClient() {
	client, err := yollavoice.NewClient(
		yollavoice.WithAPIKey("example-api-key"),
		yollavoice.WithTimeout(10*time.Second),
		yollavoice.WithMaxRetries(3),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(client.Calls != nil, client.Balance != nil, client.Pricing != nil, client.Webhooks != nil)
	// Output: true true true true
}

func ExampleCallsService_Create() {
	client := exampleClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusCreated, `{
			"id": "call_1", "status": "created", "to": "14155550100", "from": "14155550199",
			"callToken": "token-1", "maxDuration": 600,
			"createdAt": "2026-11-23T10:00:00Z", "expiresAt": "2026-11-23T10:05:00Z"}`), nil
	})

	res, err := client.Calls.Create(context.Background(), yollavoice.CreateCallParams{
		To:             "14155550100",
		ExternalUserID: "user-42",
	}, yollavoice.WithIdempotencyKey("order-A-1001"))
	if err != nil {
		fmt.Println(err)
		return
	}
	// Pass the call token to the mobile app.
	fmt.Println(res.Call.ID, res.Call.CallToken, res.Call.ExpiresAt.Format(time.RFC3339))
	// Output: call_1 token-1 2026-11-23T10:05:00Z
}

func ExampleCallsService_ListAll() {
	client := exampleClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("cursor") == "" {
			return jsonResponse(r, http.StatusOK, `{"items": [{"id": "call_1"}, {"id": "call_2"}], "nextCursor": "c2"}`), nil
		}
		return jsonResponse(r, http.StatusOK, `{"items": [{"id": "call_3"}]}`), nil
	})

	for call, err := range client.Calls.ListAll(context.Background(), yollavoice.ListCallsParams{
		Status: yollavoice.CallStatusCompleted,
		Limit:  2,
	}) {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(call.ID)
	}
	// Output:
	// call_1
	// call_2
	// call_3
}

func ExampleVerifyWebhook() {
	secret := "whsec_example"
	payload := `{"id":"evt_1","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`
	body, headers, err := yollavoice.SignWebhook([]byte(payload), secret)
	if err != nil {
		fmt.Println(err)
		return
	}

	event, err := yollavoice.VerifyWebhook(body, headers, []string{secret})
	if err != nil {
		fmt.Println(err)
		return
	}
	switch e := event.(type) {
	case *yollavoice.TestEvent:
		fmt.Println("test event", e.ID())
	case *yollavoice.CallEvent:
		fmt.Println("call event", e.Data.Call.ID)
	}
	// Output: test event evt_1
}

func ExampleNewWebhookHandler() {
	secret := "whsec_example"
	handler, err := yollavoice.NewWebhookHandler(yollavoice.WebhookHandlerConfig{
		Secrets: []string{secret},
		On: map[string]yollavoice.WebhookFunc{
			yollavoice.EventWebhookTest: func(ctx context.Context, e yollavoice.Event) error {
				fmt.Println("received", e.Type())
				return nil
			},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/yolla", handler)

	payload := `{"id":"evt_1","type":"webhook.test","seq":0,"createdAt":"2026-11-23T10:15:00Z","data":{}}`
	body, headers, err := yollavoice.SignWebhook([]byte(payload), secret)
	if err != nil {
		fmt.Println(err)
		return
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yolla", strings.NewReader(string(body)))
	req.Header = headers
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// received webhook.test
	// 200 {"received":true}
}
