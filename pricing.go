package yollavoice

import (
	"context"
	"net/http"
	"net/url"
)

// PricingService reads call rates.
type PricingService struct{ t *transport }

// GetNumberPrice returns the rate for a destination number (GET /v1/pricing/numbers/{number}).
// The number is digits only, without a leading +.
func (s *PricingService) GetNumberPrice(ctx context.Context, number string, opts ...RequestOption) (*NumberPrice, error) {
	if err := validateNumber("number", number); err != nil {
		return nil, err
	}
	rc, err := newRequestConfig(opts)
	if err != nil {
		return nil, err
	}
	req := request{requestConfig: rc, method: http.MethodGet, path: "/v1/pricing/numbers/" + url.PathEscape(number)}
	var out NumberPrice
	if _, err := s.t.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
