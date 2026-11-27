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
	req := request{requestConfig: rc, method: http.MethodGet, path: "/v1/balance"}
	var out Balance
	if _, err := s.t.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
