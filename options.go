package yollavoice

import (
	"errors"
	"net/http"
	"time"
)

// Option configures NewClient.
type Option func(*clientConfig) error

type clientConfig struct {
	apiKey         string
	webhookSecrets []string
	secretsSet     bool
	timeout        time.Duration
	maxRetries     int
	httpClient     *http.Client
}

// WithAPIKey sets the API key. An empty key falls back to the YOLLA_API_KEY environment variable.
func WithAPIKey(key string) Option {
	return func(c *clientConfig) error {
		c.apiKey = key
		return nil
	}
}

// WithWebhookSecret sets one or more webhook signing secrets (several during rotation). Empty
// secrets are rejected. Without this option the YOLLA_WEBHOOK_SECRET environment variable is used
// when it is set.
func WithWebhookSecret(secrets ...string) Option {
	return func(c *clientConfig) error {
		for _, s := range secrets {
			if s == "" {
				return errors.New("yollavoice: WithWebhookSecret: secret must not be empty")
			}
		}
		c.webhookSecrets = append([]string(nil), secrets...)
		c.secretsSet = true
		return nil
	}
}

// WithTimeout sets the per-attempt timeout. It must be positive. The default is 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) error {
		if d <= 0 {
			return errors.New("yollavoice: WithTimeout: timeout must be positive")
		}
		c.timeout = d
		return nil
	}
}

// WithMaxRetries sets the number of retries after the first attempt. It must not be negative.
// The default is 2.
func WithMaxRetries(n int) Option {
	return func(c *clientConfig) error {
		if n < 0 {
			return errors.New("yollavoice: WithMaxRetries: retries must not be negative")
		}
		c.maxRetries = n
		return nil
	}
}

// WithHTTPClient sets the HTTP client, for proxies and tests. The client is copied, never
// modified, and redirects are never followed.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) error {
		c.httpClient = hc
		return nil
	}
}

// RequestOption changes one call. An option that does not apply to a method is ignored.
type RequestOption func(*requestConfig)

// requestConfig is the result of applying RequestOptions.
type requestConfig struct {
	timeout        time.Duration // 0 = not set
	maxRetries     *int          // nil = not set
	idempotencyKey string        // "" = not set
}

// WithRequestTimeout overrides the per-attempt timeout for one call. It must be positive.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(c *requestConfig) { c.timeout = d }
}

// WithRequestMaxRetries overrides the retry count for one call. It must not be negative.
func WithRequestMaxRetries(n int) RequestOption {
	return func(c *requestConfig) { c.maxRetries = &n }
}

// WithIdempotencyKey sets the idempotency key of CallsService.Create. It is ignored by other methods.
func WithIdempotencyKey(key string) RequestOption {
	return func(c *requestConfig) { c.idempotencyKey = key }
}

// newRequestConfig applies opts and validates the result. Failures are local validation errors.
func newRequestConfig(opts []RequestOption) (requestConfig, error) {
	var c requestConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	if c.timeout < 0 {
		return c, newValidationError("WithRequestTimeout: timeout must be positive")
	}
	if c.maxRetries != nil && *c.maxRetries < 0 {
		return c, newValidationError("WithRequestMaxRetries: retries must not be negative")
	}
	return c, nil
}

// apply copies the per-request overrides into req.
func (c requestConfig) apply(req *request) {
	req.timeout = c.timeout
	req.maxRetries = c.maxRetries
}

// Ptr returns a pointer to v, for optional parameters.
func Ptr[T any](v T) *T { return &v }

// validateNumber checks a phone number against the contract Destination pattern ^[1-9][0-9]{3,14}$.
func validateNumber(field, value string) error {
	if len(value) > 0 && value[0] == '+' {
		return newValidationError(field + ": drop the leading +, use digits only")
	}
	ok := len(value) >= 4 && len(value) <= 15 && value[0] != '0'
	for i := 0; ok && i < len(value); i++ {
		ok = value[i] >= '0' && value[i] <= '9'
	}
	if !ok {
		return newValidationError(field + ": must be 4 to 15 digits in international format without a leading 0 or +")
	}
	return nil
}
