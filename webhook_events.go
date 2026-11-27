package yollavoice

import (
	"encoding/json"
	"time"
)

// Webhook event types.
const (
	EventCallInitiated  = "call.initiated"
	EventCallAnswered   = "call.answered"
	EventCallHeld       = "call.held"
	EventCallUnheld     = "call.unheld"
	EventCallHoldFailed = "call.hold_failed"
	EventCallCompleted  = "call.completed"
	EventCallExpired    = "call.expired"
	EventCallQuality    = "call.quality"
	EventBalanceLow     = "balance.low"
	EventWebhookTest    = "webhook.test"
)

// Event is a verified webhook event. Use a type switch on the concrete type: *CallEvent,
// *QualityEvent, *BalanceEvent, *TestEvent, or *UnknownEvent for types this SDK version does not
// know yet.
type Event interface {
	ID() string
	Type() string
	Seq() int64
	CreatedAt() time.Time
}

// eventEnvelope holds the fields every event has. It is embedded in the concrete events.
type eventEnvelope struct {
	id        string
	typ       string
	seq       int64
	createdAt time.Time
}

// ID returns the event id; it is the same on every redelivery.
func (e eventEnvelope) ID() string { return e.id }

// Type returns the event type, such as EventCallCompleted.
func (e eventEnvelope) Type() string { return e.typ }

// Seq returns the per-call sequence number (0 for webhook.test).
func (e eventEnvelope) Seq() int64 { return e.seq }

// CreatedAt returns when the event was created.
func (e eventEnvelope) CreatedAt() time.Time { return e.createdAt }

// CallHold describes a hold or unhold on a call.
type CallHold struct {
	Action    *CallEventDataHoldAction    `json:"action,omitempty"`
	Initiator *CallEventDataHoldInitiator `json:"initiator,omitempty"`
	Leg       *CallEventDataHoldLeg       `json:"leg,omitempty"`
}

// CallData is the data of a call lifecycle event.
type CallData struct {
	Call Call      `json:"call"`
	Hold *CallHold `json:"hold,omitempty"`
}

// QualityData is the data of a call.quality event.
type QualityData struct {
	CallID         string         `json:"callId"`
	ExternalUserID string         `json:"externalUserId"`
	Quality        QualitySummary `json:"quality"`
}

// CallEvent is a call lifecycle event: every call.* type except call.quality.
type CallEvent struct {
	eventEnvelope
	Data CallData
}

// QualityEvent is a call.quality event.
type QualityEvent struct {
	eventEnvelope
	Data QualityData
}

// BalanceEvent is a balance.low event.
type BalanceEvent struct {
	eventEnvelope
	Data Balance
}

// TestEvent is a webhook.test event, sent from the dashboard to check an endpoint.
type TestEvent struct {
	eventEnvelope
	Data map[string]any
}

// UnknownEvent is an event whose type this SDK version does not know. Data is the raw "data" member.
type UnknownEvent struct {
	eventEnvelope
	Data json.RawMessage
}

type wireEvent struct {
	ID        *string         `json:"id"`
	Type      *string         `json:"type"`
	Seq       int64           `json:"seq"`
	CreatedAt time.Time       `json:"createdAt"`
	Data      json.RawMessage `json:"data"`
}

func invalidPayload() error { return &WebhookVerificationError{Reason: ReasonInvalidPayload} }

// ParseWebhookEvent parses a webhook body into an Event without checking any signature. Use it
// only on bodies that VerifyWebhook already authenticated, or that you stored after verification.
// It returns a *WebhookVerificationError with reason invalid_payload for a body that is not an event.
func ParseWebhookEvent(body []byte) (Event, error) {
	var w wireEvent
	if err := json.Unmarshal(body, &w); err != nil || w.ID == nil || w.Type == nil {
		return nil, invalidPayload()
	}
	env := eventEnvelope{id: *w.ID, typ: *w.Type, seq: w.Seq, createdAt: w.CreatedAt}
	var (
		ev  Event
		err error
	)
	switch env.typ {
	case EventCallInitiated, EventCallAnswered, EventCallHeld, EventCallUnheld, EventCallHoldFailed,
		EventCallCompleted, EventCallExpired:
		e := &CallEvent{eventEnvelope: env}
		err, ev = json.Unmarshal(w.Data, &e.Data), e
	case EventCallQuality:
		e := &QualityEvent{eventEnvelope: env}
		err, ev = json.Unmarshal(w.Data, &e.Data), e
	case EventBalanceLow:
		e := &BalanceEvent{eventEnvelope: env}
		err, ev = json.Unmarshal(w.Data, &e.Data), e
	case EventWebhookTest:
		e := &TestEvent{eventEnvelope: env}
		if len(w.Data) > 0 {
			err = json.Unmarshal(w.Data, &e.Data)
		}
		ev = e
	default:
		ev = &UnknownEvent{eventEnvelope: env, Data: w.Data}
	}
	if err != nil {
		return nil, invalidPayload()
	}
	return ev, nil
}
