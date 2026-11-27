package yollavoice

import (
	"context"
	"net/http"
)

// BalanceService reads the partner account balance.
type BalanceService struct{ t *transport }

// Get returns the current balance (GET /v1/balance).
func (s *BalanceService) Get(ctx context.Context, opts ...RequestOption) (*Balance, error) {
	rc, err := newRequestConfig(opts)
	if err != nil {
		return nil, err
	}
	req := request{method: http.MethodGet, path: "/v1/balance"}
	rc.apply(&req)
	var out Balance
	if _, _, err := s.t.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// String returns a redacted description.
func (BalanceService) String() string { return redacted }

// GoString returns a redacted description.
func (BalanceService) GoString() string { return redacted }
