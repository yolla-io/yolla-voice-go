package yollavoice

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
)

const maxIdempotencyKeyLength = 128

// CreateCallParams are the parameters of CallsService.Create.
type CreateCallParams struct {
	// To is the destination number: 4 to 15 digits, international format, no leading 0 or +.
	To string `json:"to"`
	// From is an optional caller ID number, in the same format.
	From *string `json:"from,omitempty"`
	// ExternalUserID is your identifier of the end user.
	ExternalUserID string `json:"externalUserId"`
	// CustomParams are optional key-value pairs returned with the call.
	CustomParams map[string]string `json:"customParams,omitempty"`
	// MaxDuration is an optional call limit in seconds.
	MaxDuration *int `json:"maxDuration,omitempty"`
}

// CreateCallResult is the result of CallsService.Create.
type CreateCallResult struct {
	Call CreateCallResponse
	// Replayed is true when the API answered 200: the same idempotency key had already created
	// this call.
	Replayed bool
}

// Create creates a call (POST /v1/calls). The idempotency key defaults to a random UUID v4 that
// stays the same across the SDK's own retries; WithIdempotencyKey overrides it (at most 128
// characters).
func (s *CallsService) Create(ctx context.Context, params CreateCallParams, opts ...RequestOption) (*CreateCallResult, error) {
	rc, err := newRequestConfig(opts)
	if err != nil {
		return nil, err
	}
	key := rc.idempotencyKey
	if len(key) > maxIdempotencyKeyLength {
		return nil, newValidationError(fmt.Sprintf("idempotency key must be at most %d characters", maxIdempotencyKeyLength))
	}
	if err := validateNumber("To", params.To); err != nil {
		return nil, err
	}
	if params.From != nil {
		if err := validateNumber("From", *params.From); err != nil {
			return nil, err
		}
	}
	if key == "" {
		if key, err = newUUID(); err != nil {
			return nil, err
		}
	}
	req := request{
		method: http.MethodPost,
		path:   "/v1/calls",
		body:   params,
		header: http.Header{"Idempotency-Key": {key}},
	}
	rc.apply(&req)
	var out CreateCallResponse
	status, _, err := s.t.do(ctx, req, &out)
	if err != nil {
		return nil, err
	}
	return &CreateCallResult{Call: out, Replayed: status == http.StatusOK}, nil
}

// Get returns a call (GET /v1/calls/{id}).
func (s *CallsService) Get(ctx context.Context, callID string, opts ...RequestOption) (*Call, error) {
	return s.call(ctx, http.MethodGet, callID, "", opts)
}

// Hold puts a call on hold (POST /v1/calls/{id}/hold).
func (s *CallsService) Hold(ctx context.Context, callID string, opts ...RequestOption) (*Call, error) {
	return s.call(ctx, http.MethodPost, callID, "/hold", opts)
}

// Unhold resumes a call on hold (POST /v1/calls/{id}/unhold).
func (s *CallsService) Unhold(ctx context.Context, callID string, opts ...RequestOption) (*Call, error) {
	return s.call(ctx, http.MethodPost, callID, "/unhold", opts)
}

// End ends a call (POST /v1/calls/{id}/end).
func (s *CallsService) End(ctx context.Context, callID string, opts ...RequestOption) (*Call, error) {
	return s.call(ctx, http.MethodPost, callID, "/end", opts)
}

func (s *CallsService) call(ctx context.Context, method, callID, suffix string, opts []RequestOption) (*Call, error) {
	rc, err := newRequestConfig(opts)
	if err != nil {
		return nil, err
	}
	if callID == "" {
		return nil, newValidationError("callID must not be empty")
	}
	req := request{method: method, path: "/v1/calls/" + url.PathEscape(callID) + suffix}
	rc.apply(&req)
	var out Call
	if _, _, err := s.t.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// newUUID returns a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
