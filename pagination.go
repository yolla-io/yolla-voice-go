package yollavoice

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ListCallsParams are the filters of CallsService.List. Zero values are not sent.
type ListCallsParams struct {
	// Status keeps only calls in this status.
	Status CallStatus
	// ExternalUserID keeps only calls of this end user.
	ExternalUserID string
	// From and To bound the call creation time. They are sent in RFC 3339 with their own offset.
	From, To time.Time
	// Limit is the page size (1 to 200). The API default is 50.
	Limit int
	// Cursor starts the listing at a page returned earlier.
	Cursor string
}

// listCallsResponse is the body of GET /v1/calls (the contract defines it inline).
type listCallsResponse struct {
	Items      []Call  `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// listQuery builds the query string values of a list request.
func listQuery(p ListCallsParams) url.Values {
	q := url.Values{}
	if p.Status != "" {
		q.Set("status", string(p.Status))
	}
	if p.ExternalUserID != "" {
		q.Set("externalUserId", p.ExternalUserID)
	}
	if !p.From.IsZero() {
		q.Set("from", p.From.Format(time.RFC3339Nano))
	}
	if !p.To.IsZero() {
		q.Set("to", p.To.Format(time.RFC3339Nano))
	}
	if p.Limit != 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	return q
}

// CallPage is one page of calls.
type CallPage struct {
	Items []Call
	// NextCursor is empty on the last page.
	NextCursor string

	svc    *CallsService
	params ListCallsParams
	opts   []RequestOption
}

// HasNextPage reports whether another page follows.
func (p *CallPage) HasNextPage() bool { return p.NextCursor != "" }

// NextPage fetches the next page with the same filters and request options. It returns an
// error, without a request, when there is no next page.
func (p *CallPage) NextPage(ctx context.Context) (*CallPage, error) {
	if !p.HasNextPage() {
		return nil, newValidationError("there is no next page")
	}
	params := p.params
	params.Cursor = p.NextCursor
	return p.svc.List(ctx, params, p.opts...)
}

// List returns one page of calls (GET /v1/calls).
func (s *CallsService) List(ctx context.Context, params ListCallsParams, opts ...RequestOption) (*CallPage, error) {
	rc, err := newRequestConfig(opts)
	if err != nil {
		return nil, err
	}
	if params.Limit < 0 {
		return nil, newValidationError("Limit must not be negative")
	}
	req := request{method: http.MethodGet, path: "/v1/calls", query: listQuery(params)}
	rc.apply(&req)
	var out listCallsResponse
	if _, _, err := s.t.do(ctx, req, &out); err != nil {
		return nil, err
	}
	page := &CallPage{Items: out.Items, svc: s, params: params, opts: opts}
	if out.NextCursor != nil {
		page.NextCursor = *out.NextCursor
	}
	return page, nil
}

// ListAll iterates over every call of all pages, fetching pages lazily. A failure yields one
// (zero Call, error) pair and ends the iteration. params.Cursor applies to the first request only.
func (s *CallsService) ListAll(ctx context.Context, params ListCallsParams, opts ...RequestOption) iter.Seq2[Call, error] {
	return func(yield func(Call, error) bool) {
		page, err := s.List(ctx, params, opts...)
		for {
			if err != nil {
				yield(Call{}, err)
				return
			}
			for _, call := range page.Items {
				if !yield(call, nil) {
					return
				}
			}
			if !page.HasNextPage() {
				return
			}
			page, err = page.NextPage(ctx)
		}
	}
}
