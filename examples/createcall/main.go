// Command createcall creates a call and prints what to pass to the mobile app.
//
// Run it with YOLLA_API_KEY set.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

func main() {
	client, err := yollavoice.NewClient() // reads YOLLA_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	res, err := client.Calls.Create(ctx, yollavoice.CreateCallParams{
		To:             "14155550100",
		ExternalUserID: "user-42",
	}, yollavoice.WithIdempotencyKey("order-A-1001"))
	if err != nil {
		var apiErr *yollavoice.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("create call: status %d, code %s, request id %s", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}

	// Pass the call token to your mobile app. The app uses it to start the call.
	fmt.Println("call id:", res.Call.ID)
	fmt.Println("call token:", res.Call.CallToken)
	fmt.Println("expires at:", res.Call.ExpiresAt.Format(time.RFC3339))
	fmt.Println("replayed:", res.Replayed)
}
