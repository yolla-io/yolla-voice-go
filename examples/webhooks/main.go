// Command webhooks serves a webhook endpoint at POST /webhooks/yolla.
//
// Run it with YOLLA_WEBHOOK_SECRET set. PORT defaults to 8080.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

func main() {
	handler, err := yollavoice.NewWebhookHandler(yollavoice.WebhookHandlerConfig{
		On: map[string]yollavoice.WebhookFunc{
			yollavoice.EventCallCompleted: func(ctx context.Context, e yollavoice.Event) error {
				call := e.(*yollavoice.CallEvent).Data.Call
				log.Printf("call %s completed (event %s, seq %d)", call.ID, e.ID(), e.Seq())
				return nil
			},
			yollavoice.EventBalanceLow: func(ctx context.Context, e yollavoice.Event) error {
				log.Printf("balance low: %s", e.(*yollavoice.BalanceEvent).Data.Available.Amount)
				return nil
			},
		},
		OnUnhandled: func(ctx context.Context, e yollavoice.Event) error {
			log.Printf("ignored %s", e.Type())
			return nil
		},
		OnError: func(err error) { log.Printf("webhook error: %v", err) },
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/yolla", handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
