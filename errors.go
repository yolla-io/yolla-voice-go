package yollavoice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for use with errors.Is.
var (
	// ErrValidation matches HTTP 400 and 422 and local parameter validation failures.
	ErrValidation = errors.New("yollavoice: validation error")
	// ErrAuthentication matches HTTP 401.
	ErrAuthentication = errors.New("yollavoice: authentication error")
	// ErrInsufficientBalance matches HTTP 402.
	ErrInsufficientBalance = errors.New("yollavoice: insufficient balance")
	// ErrPermission matches HTTP 403.
	ErrPermission = errors.New("yollavoice: permission denied")
	// ErrNotFound matches HTTP 404.
	ErrNotFound = errors.New("yollavoice: not found")
	// ErrConflict matches HTTP 409.
	ErrConflict = errors.New("yollavoice: conflict")
	// ErrRateLimit matches HTTP 429.
	ErrRateLimit = errors.New("yollavoice: rate limit exceeded")
	// ErrServer matches HTTP 5xx.
	ErrServer = errors.New("yollavoice: server error")
	// ErrTimeout matches a request attempt that timed out.
	ErrTimeout = errors.New("yollavoice: request timed out")
	// ErrInvalidResponse matches a successful response whose body cannot be decoded.
	ErrInvalidResponse = errors.New("yollavoice: invalid response")
	// ErrWebhookVerification matches a *WebhookVerificationError of any reason.
	ErrWebhookVerification = errors.New("yollavoice: webhook verification failed")
)

// Webhook verification failure reasons.
const (
	ReasonMissingHeader  = "missing_header"
	ReasonBadSignature   = "bad_signature"
	ReasonStaleTimestamp = "stale_timestamp"
	ReasonInvalidPayload = "invalid_payload"
)

// APIError is an error response from the API, or a local parameter validation failure (Status 0).
// Use errors.Is with the Err* sentinels to classify it.
type APIError struct {
	// Status is the HTTP status, or 0 for local validation errors.
	Status int
	// Code is the API error code, or "unknown_error".
	Code    string
	Message string
	// RequestID identifies the request for support.
	RequestID string
	// RetryAfter is the delay from the Retry-After header of a 429 response, if present.
	RetryAfter time.Duration
}

// Error implements error. It never includes the raw response body.
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("yollavoice: ")
	b.WriteString(e.Message)
	b.WriteString(" (")
	if e.Status != 0 {
		b.WriteString("status " + strconv.Itoa(e.Status) + ", ")
	}
	b.WriteString("code " + e.Code)
	if e.RequestID != "" {
		b.WriteString(", request id " + e.RequestID)
	}
	b.WriteString(")")
	return b.String()
}

// Is matches the sentinel errors by HTTP status.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrValidation:
		return e.Status == 0 && e.Code == codeInvalidParams || e.Status == 400 || e.Status == 422
	case ErrAuthentication:
		return e.Status == 401
	case ErrInsufficientBalance:
		return e.Status == 402
	case ErrPermission:
		return e.Status == 403
	case ErrNotFound:
		return e.Status == 404
	case ErrConflict:
		return e.Status == 409
	case ErrRateLimit:
		return e.Status == 429
	case ErrServer:
		return e.Status >= 500 && e.Status <= 599
	}
	return false
}

// ConnectionError reports a request that got no HTTP response (DNS, TCP, TLS or socket failure).
type ConnectionError struct {
	Err error
}

func (e *ConnectionError) Error() string {
	return "yollavoice: connection error: " + e.Err.Error()
}

// Unwrap returns the transport error.
func (e *ConnectionError) Unwrap() error { return e.Err }

// WebhookVerificationError reports a webhook that failed verification. It never contains secrets or signatures.
type WebhookVerificationError struct {
	// Reason is one of the Reason* constants.
	Reason string
}

func (e *WebhookVerificationError) Error() string {
	return "yollavoice: webhook verification failed: " + e.Reason
}

// Is matches ErrWebhookVerification.
func (e *WebhookVerificationError) Is(target error) bool { return target == ErrWebhookVerification }

const codeInvalidParams = "invalid_params"

// newValidationError builds a local parameter validation error (status 0).
func newValidationError(message string) *APIError {
	return &APIError{Code: codeInvalidParams, Message: message}
}

// timeoutError is a per-attempt timeout. It matches ErrTimeout and context.DeadlineExceeded.
type timeoutError struct{}

func (*timeoutError) Error() string { return "yollavoice: request timed out" }

func (*timeoutError) Is(target error) bool {
	return target == ErrTimeout || target == context.DeadlineExceeded
}

// invalidResponseError is a 2xx response whose body could not be decoded.
type invalidResponseError struct {
	Status    int
	RequestID string
	Err       error
}

func (e *invalidResponseError) Error() string {
	s := fmt.Sprintf("yollavoice: invalid response (status %d", e.Status)
	if e.RequestID != "" {
		s += ", request id " + e.RequestID
	}
	s += ")"
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *invalidResponseError) Is(target error) bool { return target == ErrInvalidResponse }

func (e *invalidResponseError) Unwrap() error { return e.Err }

// parseAPIError builds the error for a non-2xx response (3xx included). now is the reference time for
// an HTTP-date Retry-After.
func parseAPIError(status int, header http.Header, body []byte, now time.Time) *APIError {
	e := &APIError{Status: status, Code: "unknown_error", Message: "HTTP " + strconv.Itoa(status)}
	var env struct {
		Error *struct {
			Code      any `json:"code"`
			Message   any `json:"message"`
			RequestID any `json:"requestId"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		code, _ := env.Error.Code.(string)
		msg, _ := env.Error.Message.(string)
		if code != "" && msg != "" {
			e.Code, e.Message = code, msg
			e.RequestID, _ = env.Error.RequestID.(string)
		}
	}
	if e.RequestID == "" {
		e.RequestID = header.Get("X-Request-Id")
	}
	if status == http.StatusTooManyRequests {
		e.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	}
	return e
}

// parseRetryAfter reads delay-seconds or an HTTP date; unreadable values give 0.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.ParseUint(value, 10, 31); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		return max(0, t.Sub(now))
	}
	return 0
}
