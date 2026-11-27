# yolla-voice-go

Go server SDK for the Yolla Voice Partner API: create and manage calls, read the balance and number prices,
and receive signed webhooks. No dependencies beyond the Go standard library.

> Pre-release (0.x). The API contract is still a draft, so minor versions may change the API.

## Installation

```sh
go get github.com/yolla-io/yolla-voice-go
```

The package name is `yollavoice`.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

func main() {
	client, err := yollavoice.NewClient() // reads YOLLA_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.Calls.Create(context.Background(), yollavoice.CreateCallParams{
		To:             "14155550100",
		ExternalUserID: "user-42",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Pass res.Call.CallToken to your mobile app. The app uses it to start the call.
	fmt.Println(res.Call.ID, res.Call.CallToken, res.Call.ExpiresAt)
}
```

## Configuration

```go
client, err := yollavoice.NewClient(
	yollavoice.WithAPIKey("your-api-key"),
	yollavoice.WithTimeout(10*time.Second),
	yollavoice.WithMaxRetries(3),
)
_, _ = client, err
```

| Option                    | Environment variable   | Default | Description                                                  |
| ------------------------- | ---------------------- | ------- | ------------------------------------------------------------ |
| `WithAPIKey`              | `YOLLA_API_KEY`        | none    | Partner API key. Required.                                   |
| `WithWebhookSecret`       | `YOLLA_WEBHOOK_SECRET` | none    | Signing secret, or several during rotation.                  |
| `WithTimeout`             |                        | `30s`   | Timeout of one attempt.                                      |
| `WithMaxRetries`          |                        | `2`     | Retries after a 429, 5xx, network error or timeout.          |
| `WithHTTPClient`          |                        | default | Custom `*http.Client`, for proxies or tests.                 |

Requests go to `https://platform.yolla.io/api`. `NewClient` returns an error when the API key is missing.
The API key never appears in error messages or in `%v`, `%+v`, `%#v` and JSON output of the client or of any
error.

`WithHTTPClient` copies the client and never follows redirects. If that client has its own `Timeout`, an
expired timeout surfaces as a connection error, not as `ErrTimeout`. Prefer `WithTimeout` and
`WithRequestTimeout`, which also drive the retries.

Every method takes the `context.Context` as its first argument and optional per-call options. A canceled
context stops the SDK at once, also during a backoff pause.

```go
balance, err := client.Balance.Get(ctx,
	yollavoice.WithRequestTimeout(5*time.Second),
	yollavoice.WithRequestMaxRetries(0),
)
_, _ = balance, err
```

## Calls

```go
// Create. The result says whether the call already existed for this idempotency key.
res, err := client.Calls.Create(ctx, yollavoice.CreateCallParams{
	To:             "14155550100",
	From:           yollavoice.Ptr("14155550199"), // optional caller id
	ExternalUserID: "user-42",
	CustomParams:   map[string]string{"orderId": "A-1001"},
	MaxDuration:    yollavoice.Ptr(600), // seconds, optional
}, yollavoice.WithIdempotencyKey("order-A-1001")) // optional, a random UUID by default
if err != nil {
	return
}
fmt.Println(res.Call.CallToken, res.Call.ExpiresAt, res.Replayed)

id := res.Call.ID
current, _ := client.Calls.Get(ctx, id)
held, _ := client.Calls.Hold(ctx, id)
resumed, _ := client.Calls.Unhold(ctx, id)
ended, _ := client.Calls.End(ctx, id)
_, _, _, _ = current, held, resumed, ended

// Balance and prices
balance, _ := client.Balance.Get(ctx)                              // Balance, Available, CreditLimit, BillingMode
price, _ := client.Pricing.GetNumberPrice(ctx, "14155550100")      // Number, Country, Rate
_, _ = balance, price
```

`Calls.Create` returns the response of the create operation: `CallToken`, `ExpiresAt` and the rest of
`CreateCallResponse` in `res.Call`, and `Replayed`. The other call methods return a `*Call`.

Optional parameters are pointers. `yollavoice.Ptr` makes one from any value.

Phone numbers are digits only, without a leading `+`. A number with `+` is rejected with an error that matches
`ErrValidation`, before any request is sent.

## Pagination

`Calls.ListAll` returns an iterator over every call of every page. Pages are fetched as you iterate. A failure
yields one `(Call{}, err)` pair and ends the loop.

```go
// Every call, fetching the next page only when needed
for call, err := range client.Calls.ListAll(ctx, yollavoice.ListCallsParams{
	Status: yollavoice.CallStatusCompleted,
	Limit:  100,
}) {
	if err != nil {
		return
	}
	fmt.Println(call.ID, call.Duration)
}

// Page by page
page, err := client.Calls.List(ctx, yollavoice.ListCallsParams{ExternalUserID: "user-42"})
for err == nil {
	fmt.Println(len(page.Items))
	if !page.HasNextPage() {
		break
	}
	page, err = page.NextPage(ctx)
}
```

Filters: `Status`, `ExternalUserID`, `From`, `To`, `Limit`, `Cursor`. Zero values are not sent. In `ListAll`,
`Cursor` applies to the first request only.

## Errors

API failures are `*yollavoice.APIError` with `Status`, `Code`, `Message`, `RequestID` and `RetryAfter`. Quote
the `RequestID` in support requests. Classify with `errors.Is` and the sentinels, then branch on `Code` for the
exact reason.

| Sentinel                 | When                                                        |
| ------------------------ | ----------------------------------------------------------- |
| `ErrValidation`          | 400, 422: malformed request or invalid parameters           |
| `ErrAuthentication`      | 401: missing or invalid API key                             |
| `ErrInsufficientBalance` | 402: not enough balance                                     |
| `ErrPermission`          | 403: blocked partner, user, destination or caller id        |
| `ErrNotFound`            | 404: call not found                                         |
| `ErrConflict`            | 409: idempotency conflict or wrong call state               |
| `ErrRateLimit`           | 429: rate limit or concurrent calls; `RetryAfter` is set    |
| `ErrServer`              | 5xx                                                         |
| `ErrTimeout`             | no response within the per-attempt timeout                  |
| `ErrInvalidResponse`     | the response is not what the contract describes             |
| `ErrWebhookVerification` | webhook failed verification; `*WebhookVerificationError`    |

`*ConnectionError` reports a request without any HTTP response (DNS, TCP, TLS); the transport error is
available through `errors.Unwrap`.

```go
_, err := client.Calls.Create(ctx, yollavoice.CreateCallParams{To: "14155550100", ExternalUserID: "user-42"})

var apiErr *yollavoice.APIError
switch {
case err == nil:
case errors.Is(err, yollavoice.ErrPermission) && errors.As(err, &apiErr) && apiErr.Code == "destination_blocked":
	// tell the user this destination is not available
case errors.Is(err, yollavoice.ErrConflict) && errors.As(err, &apiErr) && apiErr.Code == "idempotency_conflict":
	// the same idempotency key was used with different parameters
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Status, apiErr.Code, apiErr.RequestID)
default:
	fmt.Println(err)
}
```

## Retries and idempotency

The SDK retries a 429, a 5xx, a network error and a per-attempt timeout, up to the configured number of
retries, with exponential backoff and full jitter (from 0.5 s, capped at 8 s). It honors `Retry-After`, capped at
60 s. Other 4xx answers are never retried. When the retries run out, the returned error is the one from the
last attempt, with its `RequestID`.

`Calls.Create` sends an `Idempotency-Key` header. The SDK generates one per call and reuses it on its own
retries, so a retried create never starts a second call. To make your own retries safe too, pass the same key
(for example, your order id), at most 128 characters:

```go
res, err := client.Calls.Create(ctx, yollavoice.CreateCallParams{To: "14155550100", ExternalUserID: "user-42"},
	yollavoice.WithIdempotencyKey("order-A-1001"))
if err == nil && res.Replayed {
	// The call was created by an earlier request with this key.
	// res.Call.CallToken may be spent or expired: check res.Call.ExpiresAt and create a new call if needed.
}
```

`Hold`, `Unhold` and `End` are retried too. If the first attempt reached the API but the response was lost,
the retry can answer `409 invalid_call_state` (for example, a second `Hold`). Treat it as "already done" after
reading the call with `Calls.Get`.

## Webhooks

The platform sends events to your endpoint as `POST` with a JSON body and these headers:

| Header                | Value                                                                  |
| --------------------- | ---------------------------------------------------------------------- |
| `X-Voice-Timestamp`   | Unix time in seconds                                                   |
| `X-Voice-Signature`   | `v1=` plus hex HMAC-SHA256 of `<timestamp>.<raw body>` with the secret |
| `X-Voice-Delivery-Id` | Id of this delivery attempt                                            |

Verification needs the raw body, exactly as received. Do not parse it first.

### net/http

`NewWebhookHandler` returns an `http.Handler`. Mount it with a `POST` pattern of `http.ServeMux`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

func main() {
	handler, err := yollavoice.NewWebhookHandler(yollavoice.WebhookHandlerConfig{
		// Secrets defaults to YOLLA_WEBHOOK_SECRET.
		On: map[string]yollavoice.WebhookFunc{
			yollavoice.EventCallCompleted: func(ctx context.Context, e yollavoice.Event) error {
				call := e.(*yollavoice.CallEvent).Data.Call
				fmt.Println(call.ID, call.BilledDuration)
				return nil
			},
			yollavoice.EventBalanceLow: func(ctx context.Context, e yollavoice.Event) error {
				fmt.Println(e.(*yollavoice.BalanceEvent).Data.Available.Amount)
				return nil
			},
		},
		OnUnhandled: func(ctx context.Context, e yollavoice.Event) error {
			fmt.Println("ignored", e.Type())
			return nil
		},
		OnError: func(err error) { log.Println(err) },
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/yolla", handler)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

The handler answers:

| Status | When                                                                                                    |
| ------ | ------------------------------------------------------------------------------------------------------- |
| 200    | the event is verified and your function returned `nil`, or no function matches and `OnUnhandled` is nil |
| 401    | bad signature, missing header, stale timestamp or invalid payload                                       |
| 405    | the method is not `POST` (the `Allow` header says `POST`)                                               |
| 413    | the body is over 1 MiB (`MaxBodyBytes`)                                                                 |
| 500    | your function returned an error or panicked; or the request body could not be read                      |

On a 200 the body is `{"received":true}`. `OnError` receives the real error of every 401 (a
`*WebhookVerificationError` with its `Reason`: log it to find a wrong secret or clock skew) and of every 500.
The response body never contains it.

The secret defaults to `YOLLA_WEBHOOK_SECRET`. `NewWebhookHandler` checks it when you create the handler and
returns an error if there is none, so a misconfigured service fails at startup. If you already created a client
with `WithWebhookSecret`, use `client.Webhooks.NewHandler(cfg)`.

### Verifying yourself

```go
event, err := client.Webhooks.Verify(rawBody, r.Header) // rawBody is the exact request body
if err != nil {
	var verr *yollavoice.WebhookVerificationError
	if errors.As(err, &verr) {
		fmt.Println(verr.Reason) // ReasonMissingHeader, ReasonBadSignature, ReasonStaleTimestamp, ReasonInvalidPayload
	}
	return
}
switch e := event.(type) {
case *yollavoice.CallEvent:
	fmt.Println(e.Type(), e.Data.Call.EndReason)
case *yollavoice.QualityEvent:
	fmt.Println(e.Data.Quality.MOSEstimate)
case *yollavoice.BalanceEvent:
	fmt.Println(e.Data.Available.Amount)
case *yollavoice.TestEvent:
	fmt.Println("test event", e.ID())
case *yollavoice.UnknownEvent:
	// Event types added later arrive here. Data is the raw "data" member.
	fmt.Println("new event type", e.Type(), string(e.Data))
}
```

`yollavoice.VerifyWebhook(body, header, secrets, opts...)` does the same without a client; empty `secrets` use
`YOLLA_WEBHOOK_SECRET`. Header names match in any case. The timestamp must be within 5 minutes of your clock
(`WithTolerance`).

### Events

`EventCallInitiated`, `EventCallAnswered`, `EventCallHeld`, `EventCallUnheld`, `EventCallHoldFailed`,
`EventCallCompleted`, `EventCallExpired`, `EventCallQuality`, `EventBalanceLow`, `EventWebhookTest`
(`call.initiated`, `call.answered`, `call.held`, `call.unheld`, `call.hold_failed`, `call.completed`,
`call.expired`, `call.quality`, `balance.low`, `webhook.test`).

Every event has `ID()`, `Type()`, `Seq()` and `CreatedAt()`. All `call.*` types except `call.quality` arrive as
`*CallEvent`.

- **Retries and duplicates.** The platform retries a delivery when your endpoint answers non-2xx or does not
  answer. `ID()` is the same across attempts, and `X-Voice-Delivery-Id` is new for each attempt. Deduplicate by
  event id.
- **Order.** Deliveries can arrive out of order. For `*CallEvent`, `Seq()` orders the events of one call: apply
  the event only if its `Seq()` is greater than the last one you stored. `Seq()` is 0 for `call.quality`,
  `balance.low` and `webhook.test`.
- **Answer fast.** Store the event and return `nil`, then do slow work in a queue. `json.Marshal(e)` writes the
  event in its wire shape (id, type, seq, createdAt, data), and `ParseWebhookEvent` reads it back.
- **Secret rotation.** Rotate the secret in the dashboard. The new secret works immediately. During your own
  switch, pass both: `Secrets: []string{newSecret, oldSecret}` (or `WithWebhookSecret(newSecret, oldSecret)`).
  A signature matching any of them is accepted. Remove the old one when done.

## Testing webhooks

In the dashboard, open Integration and press "Send test event". It sends a `webhook.test` event to your
endpoint.

In unit tests, sign a payload the way the platform does:

```go
secret := "whsec_test"
handler, err := yollavoice.NewWebhookHandler(yollavoice.WebhookHandlerConfig{
	Secrets: []string{secret},
	On: map[string]yollavoice.WebhookFunc{
		yollavoice.EventWebhookTest: func(ctx context.Context, e yollavoice.Event) error { return nil },
	},
})
if err != nil {
	return
}

payload := map[string]any{
	"id": "evt_1", "type": "webhook.test", "seq": 0,
	"createdAt": time.Now().UTC().Format(time.RFC3339), "data": map[string]any{},
}
body, headers, err := yollavoice.SignWebhook(payload, secret)
if err != nil {
	return
}

req := httptest.NewRequest(http.MethodPost, "/webhooks/yolla", strings.NewReader(string(body)))
req.Header = headers
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)
fmt.Println(rec.Code) // 200
```

With a client, `client.Webhooks.Sign(payload)` signs with the client secret.

## Requirements

- Go 1.26 or newer.
- Server-side only. The API key can start calls and spend money: never ship it to a browser or a mobile app.
  Give the app only the `CallToken` of a call.

## Versioning

The module follows semantic versioning. While the API contract is a draft, versions are 0.x and a minor release
may include breaking changes. They are listed in [CHANGELOG.md](CHANGELOG.md). `yollavoice.Version` holds the
current version.

## License

MIT
