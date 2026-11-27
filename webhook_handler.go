package yollavoice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultMaxBodyBytes int64 = 1 << 20

const (
	bodyReceived     = `{"received":true}`
	bodyInvalidSig   = `{"error":"invalid signature"}`
	bodyBadMethod    = `{"error":"method not allowed"}`
	bodyTooLarge     = `{"error":"payload too large"}`
	bodyHandlerError = `{"error":"handler failed"}`
)

// WebhookFunc handles one verified webhook event. ctx is the request context. A returned error
// (or a panic) answers 500, so the platform retries the delivery.
type WebhookFunc func(ctx context.Context, e Event) error

// WebhookHandlerConfig configures NewWebhookHandler.
type WebhookHandlerConfig struct {
	// Secrets are the webhook secrets; several during rotation. Empty uses YOLLA_WEBHOOK_SECRET.
	Secrets []string
	// On maps an event type (for example EventCallCompleted) to its handler. Types this SDK
	// version does not know are allowed.
	On map[string]WebhookFunc
	// OnUnhandled receives events without an On entry, including unknown types. Optional.
	OnUnhandled WebhookFunc
	// OnError receives the real error of every 401 and 500 answer. The response never contains
	// it. A panic inside OnError is recovered and ignored. Optional.
	OnError func(error)
	// MaxBodyBytes limits the request body. Zero means 1 MiB; negative is an error.
	MaxBodyBytes int64
	// Tolerance is the allowed distance between the timestamp header and the clock. Zero means 5
	// minutes; negative is an error.
	Tolerance time.Duration
}

// WebhookHandler is an http.Handler that verifies and dispatches platform webhooks. It is safe
// for concurrent use.
type WebhookHandler struct {
	secrets     []string
	on          map[string]WebhookFunc
	onUnhandled WebhookFunc
	onError     func(error)
	maxBody     int64
	tolerance   time.Duration
}

// NewWebhookHandler checks the configuration and returns a handler. It fails when no secret is
// available or a limit is negative.
func NewWebhookHandler(cfg WebhookHandlerConfig) (*WebhookHandler, error) {
	secrets, err := resolveSecrets(cfg.Secrets)
	if err != nil {
		return nil, err
	}
	if cfg.MaxBodyBytes < 0 {
		return nil, errors.New("yollavoice: WebhookHandlerConfig.MaxBodyBytes must not be negative")
	}
	if cfg.Tolerance < 0 {
		return nil, errors.New("yollavoice: WebhookHandlerConfig.Tolerance must not be negative")
	}
	h := &WebhookHandler{
		secrets:     secrets,
		on:          make(map[string]WebhookFunc, len(cfg.On)),
		onUnhandled: cfg.OnUnhandled,
		onError:     cfg.OnError,
		maxBody:     cfg.MaxBodyBytes,
		tolerance:   cfg.Tolerance,
	}
	for k, f := range cfg.On {
		if f != nil {
			h.on[k] = f
		}
	}
	if h.maxBody == 0 {
		h.maxBody = defaultMaxBodyBytes
	}
	if h.tolerance == 0 {
		h.tolerance = defaultTolerance
	}
	return h, nil
}

// Format implements fmt.Formatter with a redacted description for every verb, so secrets never
// reach logs.
func (WebhookHandler) Format(s fmt.State, _ rune) {
	writeRedacted(s, "yollavoice.WebhookHandler{redacted}")
}

const redactedHandlerConfig = "yollavoice.WebhookHandlerConfig{redacted}"

// Format implements fmt.Formatter with a redacted description for every verb, so secrets never
// reach logs.
func (WebhookHandlerConfig) Format(s fmt.State, _ rune) { writeRedacted(s, redactedHandlerConfig) }

// MarshalJSON returns a redacted JSON string.
func (WebhookHandlerConfig) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactedHandlerConfig + `"`), nil
}

// NewHandler builds a webhook handler. When cfg.Secrets is empty it uses the client's webhook
// secrets. See NewWebhookHandler.
func (w *WebhooksService) NewHandler(cfg WebhookHandlerConfig) (*WebhookHandler, error) {
	if len(cfg.Secrets) == 0 {
		cfg.Secrets = w.secrets
	}
	return NewWebhookHandler(cfg)
}

func (h *WebhookHandler) report(err error) {
	if h.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	h.onError(err)
}

func writeWebhookJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// ServeHTTP implements http.Handler.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeWebhookJSON(w, http.StatusMethodNotAllowed, bodyBadMethod)
		return
	}
	if r.ContentLength > h.maxBody {
		writeWebhookJSON(w, http.StatusRequestEntityTooLarge, bodyTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeWebhookJSON(w, http.StatusRequestEntityTooLarge, bodyTooLarge)
			return
		}
		h.report(err)
		writeWebhookJSON(w, http.StatusInternalServerError, bodyHandlerError)
		return
	}
	event, err := VerifyWebhook(body, r.Header, h.secrets, WithTolerance(h.tolerance))
	if err != nil {
		h.report(err)
		if errors.Is(err, ErrWebhookVerification) {
			writeWebhookJSON(w, http.StatusUnauthorized, bodyInvalidSig)
		} else {
			writeWebhookJSON(w, http.StatusInternalServerError, bodyHandlerError)
		}
		return
	}
	fn := h.on[event.Type()]
	if fn == nil {
		fn = h.onUnhandled
	}
	if fn != nil {
		if err := callSafely(r.Context(), fn, event); err != nil {
			h.report(err)
			writeWebhookJSON(w, http.StatusInternalServerError, bodyHandlerError)
			return
		}
	}
	writeWebhookJSON(w, http.StatusOK, bodyReceived)
}

// callSafely runs fn and turns a panic into an error that carries the panic value (no stack).
// A panic with http.ErrAbortHandler is passed on, so it still aborts the connection.
func callSafely(ctx context.Context, fn WebhookFunc, e Event) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if p == http.ErrAbortHandler {
				panic(p)
			}
			if pe, ok := p.(error); ok {
				err = fmt.Errorf("yollavoice: webhook handler panicked: %w", pe)
			} else {
				err = fmt.Errorf("yollavoice: webhook handler panicked: %v", p)
			}
		}
	}()
	return fn(ctx, e)
}
