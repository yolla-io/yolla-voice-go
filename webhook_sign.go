package yollavoice

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type signConfig struct {
	ts    time.Time
	tsSet bool
}

// SignOption configures SignWebhook.
type SignOption func(*signConfig)

// WithSignTimestamp signs with t instead of the current time, which makes the output deterministic.
func WithSignTimestamp(t time.Time) SignOption {
	return func(c *signConfig) { c.ts, c.tsSet = t, true }
}

// SignWebhook signs a payload the way the platform does, for tests of your webhook endpoint. A
// []byte or json.RawMessage payload is signed as is; anything else is marshaled to JSON. An empty
// secret uses YOLLA_WEBHOOK_SECRET. It returns the body and the X-Voice-Timestamp and
// X-Voice-Signature headers.
func SignWebhook(payload any, secret string, opts ...SignOption) ([]byte, http.Header, error) {
	secrets, err := resolveSecrets([]string{secret})
	if err != nil {
		return nil, nil, err
	}
	var cfg signConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	var body []byte
	switch p := payload.(type) {
	case []byte:
		body = p
	case json.RawMessage:
		body = p
	default:
		if body, err = json.Marshal(payload); err != nil {
			return nil, nil, err
		}
	}
	ts := webhookNow()
	if cfg.tsSet {
		ts = cfg.ts
	}
	stamp := strconv.FormatInt(ts.Unix(), 10)
	h := http.Header{}
	h.Set(timestampHeader, stamp)
	h.Set(signatureHeader, "v1="+computeSignature(secrets[0], stamp, body))
	return body, h, nil
}
