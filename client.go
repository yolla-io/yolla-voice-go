package yollavoice

import (
	"errors"
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

	t              *transport
	webhookSecrets []string
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
		Calls:          &CallsService{t: t},
		Balance:        &BalanceService{t: t},
		Pricing:        &PricingService{t: t},
		Webhooks:       &WebhooksService{secrets: cfg.webhookSecrets},
		t:              t,
		webhookSecrets: cfg.webhookSecrets,
	}, nil
}

// String returns a redacted description.
func (c Client) String() string { return redacted }

// GoString returns a redacted description.
func (c Client) GoString() string { return redacted }

// MarshalJSON returns a redacted JSON string.
func (c Client) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// String returns a redacted description, so the API key never reaches logs.
func (t transport) String() string { return "yollavoice.transport{redacted}" }

// GoString returns a redacted description.
func (t transport) GoString() string { return t.String() }

// String returns a redacted description.
func (CallsService) String() string { return redacted }

// GoString returns a redacted description.
func (CallsService) GoString() string { return redacted }

// String returns a redacted description, so webhook secrets never reach logs.
func (WebhooksService) String() string { return redacted }

// GoString returns a redacted description.
func (WebhooksService) GoString() string { return redacted }
