package yollavoice

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
	redacted          = "yollavoice.Client{redacted}"
)

// Client is the Yolla Voice partner API client. It is safe for concurrent use and holds no
// resources that need closing.
type Client struct {
	Calls    *CallsService
	Balance  *BalanceService
	Pricing  *PricingService
	Webhooks *WebhooksService
}

// CallsService groups the call operations.
type CallsService struct{ t *transport }

// WebhooksService groups webhook verification and parsing.
type WebhooksService struct{ secrets []string }

// NewClient builds a client. The API key comes from WithAPIKey or the YOLLA_API_KEY environment
// variable.
func NewClient(opts ...Option) (*Client, error) {
	cfg := clientConfig{timeout: defaultTimeout, maxRetries: defaultMaxRetries}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("YOLLA_API_KEY")
	}
	if cfg.apiKey == "" {
		return nil, errors.New("yollavoice: missing API key: use WithAPIKey or set YOLLA_API_KEY")
	}
	if !cfg.secretsSet {
		if s := os.Getenv("YOLLA_WEBHOOK_SECRET"); s != "" {
			cfg.webhookSecrets = []string{s}
		}
	}
	t := newTransport(cfg.httpClient, cfg.apiKey, cfg.timeout, cfg.maxRetries)
	return &Client{
		Calls:    &CallsService{t: t},
		Balance:  &BalanceService{t: t},
		Pricing:  &PricingService{t: t},
		Webhooks: &WebhooksService{secrets: cfg.webhookSecrets},
	}, nil
}

// String returns a redacted description.
func (c Client) String() string { return redacted }

// GoString returns a redacted description.
func (c Client) GoString() string { return redacted }

// MarshalJSON returns a redacted JSON string.
func (c Client) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Format implements fmt.Formatter: every verb (%d and %x included) prints the redacted
// description, so the API key and webhook secrets never reach logs.
func (c Client) Format(s fmt.State, _ rune) { writeRedacted(s, redacted) }

// Format implements fmt.Formatter with a redacted description for every verb.
func (CallsService) Format(s fmt.State, _ rune) {
	writeRedacted(s, "yollavoice.CallsService{redacted}")
}

// Format implements fmt.Formatter with a redacted description for every verb.
func (BalanceService) Format(s fmt.State, _ rune) {
	writeRedacted(s, "yollavoice.BalanceService{redacted}")
}

// Format implements fmt.Formatter with a redacted description for every verb.
func (PricingService) Format(s fmt.State, _ rune) {
	writeRedacted(s, "yollavoice.PricingService{redacted}")
}

// Format implements fmt.Formatter with a redacted description for every verb, so webhook
// secrets never reach logs.
func (WebhooksService) Format(s fmt.State, _ rune) {
	writeRedacted(s, "yollavoice.WebhooksService{redacted}")
}

// Format keeps the API key out of every fmt verb.
func (transport) Format(s fmt.State, _ rune) { writeRedacted(s, "yollavoice.transport{redacted}") }

// writeRedacted writes text for a Format method; fmt.State has no error to report.
func writeRedacted(s fmt.State, text string) { _, _ = io.WriteString(s, text) }

// Verify checks a webhook request with the client's webhook secrets. A client created without a
// secret reads YOLLA_WEBHOOK_SECRET at call time instead, and Verify returns an error when that
// is not set either. See VerifyWebhook.
func (w *WebhooksService) Verify(body []byte, header http.Header, opts ...VerifyOption) (Event, error) {
	secrets, err := resolveSecrets(w.secrets)
	if err != nil {
		return nil, err
	}
	return VerifyWebhook(body, header, secrets, opts...)
}

// Sign signs a payload for tests with the client's first webhook secret. A client created without
// a secret reads YOLLA_WEBHOOK_SECRET at call time instead, and Sign returns an error when that is
// not set either. See SignWebhook.
func (w *WebhooksService) Sign(payload any, opts ...SignOption) ([]byte, http.Header, error) {
	secrets, err := resolveSecrets(w.secrets)
	if err != nil {
		return nil, nil, err
	}
	return SignWebhook(payload, secrets[0], opts...)
}
