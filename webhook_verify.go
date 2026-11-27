package yollavoice

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	timestampHeader  = "X-Voice-Timestamp"
	signatureHeader  = "X-Voice-Signature"
	defaultTolerance = 300 * time.Second
)

// webhookNow is the clock of verification and signing; tests replace it.
var webhookNow = time.Now

type verifyConfig struct{ tolerance time.Duration }

// VerifyOption configures VerifyWebhook.
type VerifyOption func(*verifyConfig)

// WithTolerance sets how far the timestamp header may be from the clock. The default is 5 minutes.
func WithTolerance(d time.Duration) VerifyOption {
	return func(c *verifyConfig) { c.tolerance = d }
}

// resolveSecrets returns the secrets to use. An empty list, or a list of one empty string, falls
// back to YOLLA_WEBHOOK_SECRET. It returns a plain error (not a *WebhookVerificationError) when
// no secret is available or a list holds an empty one.
func resolveSecrets(secrets []string) ([]string, error) {
	if len(secrets) == 0 || (len(secrets) == 1 && secrets[0] == "") {
		if env := os.Getenv("YOLLA_WEBHOOK_SECRET"); env != "" {
			return []string{env}, nil
		}
		return nil, errors.New("yollavoice: missing webhook secret: pass a secret or set YOLLA_WEBHOOK_SECRET")
	}
	for _, s := range secrets {
		if s == "" {
			return nil, errors.New("yollavoice: webhook secret must not be empty")
		}
	}
	return secrets, nil
}

func computeSignature(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// headerValue reads a header case-insensitively, first value, trimmed.
func headerValue(h http.Header, name string) string {
	if v := h.Get(name); v != "" {
		return strings.TrimSpace(v)
	}
	for k, v := range h {
		if len(v) > 0 && strings.EqualFold(k, name) {
			return strings.TrimSpace(v[0])
		}
	}
	return ""
}

// VerifyWebhook checks the timestamp and signature of a webhook request and returns the parsed
// event. body must be the raw request body. The signature must match any one of secrets (several
// during rotation); nil or empty secrets use YOLLA_WEBHOOK_SECRET. Failures are
// *WebhookVerificationError (matching ErrWebhookVerification) with a Reason; a missing secret is
// a different, plain error.
func VerifyWebhook(body []byte, header http.Header, secrets []string, opts ...VerifyOption) (Event, error) {
	secrets, err := resolveSecrets(secrets)
	if err != nil {
		return nil, err
	}
	cfg := verifyConfig{tolerance: defaultTolerance}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	ts, sig := headerValue(header, timestampHeader), headerValue(header, signatureHeader)
	if ts == "" || sig == "" {
		return nil, &WebhookVerificationError{Reason: ReasonMissingHeader}
	}
	stale := &WebhookVerificationError{Reason: ReasonStaleTimestamp}
	for _, r := range ts {
		if r < '0' || r > '9' {
			return nil, stale
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, stale
	}
	diff := webhookNow().Sub(time.Unix(sec, 0))
	if diff < 0 {
		diff = -diff
	}
	if diff > cfg.tolerance {
		return nil, stale
	}
	matched := false
	for _, s := range secrets {
		if hmac.Equal([]byte("v1="+computeSignature(s, ts, body)), []byte(sig)) {
			matched = true
		}
	}
	if !matched {
		return nil, &WebhookVerificationError{Reason: ReasonBadSignature}
	}
	return ParseWebhookEvent(body)
}
