// Package yollavoice is a server SDK for the Yolla Voice Partner API: calls,
// balance, pricing and signed webhooks. It has no dependencies beyond the Go
// standard library.
//
// Create a client with [NewClient]; it reads the API key from WithAPIKey or the
// YOLLA_API_KEY environment variable. The client exposes four services:
// Calls (create, get, hold, unhold, end, list), Balance, Pricing and Webhooks.
// Every method takes a context.Context first. Failures are *APIError values
// that match the Err* sentinels with errors.Is.
//
// Webhooks are verified with [VerifyWebhook] or served by [NewWebhookHandler],
// which is an http.Handler. [SignWebhook] signs payloads for tests.
//
// The README covers installation, configuration, errors, retries and
// idempotency, pagination, webhooks and testing; see the sections named
// Quick start, Configuration, Calls, Pagination, Errors, Retries and
// idempotency, Webhooks and Testing webhooks.
package yollavoice
