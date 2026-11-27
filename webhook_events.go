package yollavoice

import (
	"encoding/json"
	"errors"
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
	EventCallBilled     = "call.billed"
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

// eventEnvelope holds the fields every event has. It is embedded in the concrete events. Its
// methods have pointer receivers, so only the pointer event types (*CallEvent and so on)
// implement Event, and a type switch case on a non-pointer event type does not compile.
type eventEnvelope struct {
	id        string
	typ       string
	seq       int64
	createdAt time.Time
}

// ID returns the event id; it is the same on every redelivery.
func (e *eventEnvelope) ID() string { return e.id }

// Type returns the event type, such as EventCallCompleted.
func (e *eventEnvelope) Type() string { return e.typ }

// Seq returns the per-call sequence number (0 for webhook.test).
func (e *eventEnvelope) Seq() int64 { return e.seq }

// CreatedAt returns when the event was created.
func (e *eventEnvelope) CreatedAt() time.Time { return e.createdAt }

// marshalEvent writes the wire shape of an event: id, type, seq, createdAt and data. data is
// left out when it is nil.
func marshalEvent(env eventEnvelope, data any) ([]byte, error) {
	return json.Marshal(struct {
		ID        string    `json:"id"`
		Type      string    `json:"type"`
		Seq       int64     `json:"seq"`
		CreatedAt time.Time `json:"createdAt,omitzero"`
		Data      any       `json:"data,omitempty"`
	}{env.id, env.typ, env.seq, env.createdAt, data})
}

// HoldAction is the action of a hold change.
type HoldAction string

// Hold actions.
const (
	HoldActionHold   HoldAction = "hold"
	HoldActionUnhold HoldAction = "unhold"
)

// HoldInitiator is the side that asked for a hold change.
type HoldInitiator string

// Hold initiators.
const (
	HoldInitiatorClient HoldInitiator = "client"
	HoldInitiatorServer HoldInitiator = "server"
)

// HoldLeg is the call leg put on hold.
type HoldLeg string

// Hold legs.
const (
	HoldLegCaller HoldLeg = "caller"
	HoldLegCallee HoldLeg = "callee"
)

// CallHold describes a hold or unhold on a call.
type CallHold struct {
	Action    *HoldAction    `json:"action,omitempty"`
	Initiator *HoldInitiator `json:"initiator,omitempty"`
	Leg       *HoldLeg       `json:"leg,omitempty"`
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

// MarshalJSON writes the event in its wire shape, so a stored event keeps its id, type, seq and
// createdAt, and ParseWebhookEvent reads it back.
func (e CallEvent) MarshalJSON() ([]byte, error) { return marshalEvent(e.eventEnvelope, e.Data) }

// MarshalJSON writes the event in its wire shape. See CallEvent.MarshalJSON.
func (e QualityEvent) MarshalJSON() ([]byte, error) { return marshalEvent(e.eventEnvelope, e.Data) }

// MarshalJSON writes the event in its wire shape. See CallEvent.MarshalJSON.
func (e BalanceEvent) MarshalJSON() ([]byte, error) { return marshalEvent(e.eventEnvelope, e.Data) }

// MarshalJSON writes the event in its wire shape. See CallEvent.MarshalJSON.
func (e TestEvent) MarshalJSON() ([]byte, error) { return marshalEvent(e.eventEnvelope, e.Data) }

// MarshalJSON writes the event in its wire shape with Data as it was received. See
// CallEvent.MarshalJSON.
func (e UnknownEvent) MarshalJSON() ([]byte, error) {
	if len(e.Data) == 0 {
		return marshalEvent(e.eventEnvelope, nil)
	}
	return marshalEvent(e.eventEnvelope, e.Data)
}

type wireEvent struct {
	ID        *string         `json:"id"`
	Type      *string         `json:"type"`
	Seq       json.RawMessage `json:"seq"`
	CreatedAt json.RawMessage `json:"createdAt"`
	Data      json.RawMessage `json:"data"`
}

func invalidPayload() error { return &WebhookVerificationError{Reason: ReasonInvalidPayload} }

// decodeIfPresent decodes raw into v unless the member was absent.
func decodeIfPresent(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// ParseWebhookEvent parses a webhook body into an Event without checking any signature. Use it
// only on bodies that VerifyWebhook already authenticated, or that you stored after verification.
// It returns a *WebhookVerificationError with reason invalid_payload for a body that is not an
// event: the envelope needs string id and type, and a known type must match its contract. For an
// unknown type, seq and createdAt are read when they are valid and left zero otherwise.
func ParseWebhookEvent(body []byte) (Event, error) {
	var w wireEvent
	if err := json.Unmarshal(body, &w); err != nil || w.ID == nil || w.Type == nil {
		return nil, invalidPayload()
	}
	env := eventEnvelope{id: *w.ID, typ: *w.Type}
	envErr := errors.Join(decodeIfPresent(w.Seq, &env.seq), decodeIfPresent(w.CreatedAt, &env.createdAt))
	var (
		ev  Event
		err error
	)
	switch env.typ {
	case EventCallInitiated, EventCallAnswered, EventCallHeld, EventCallUnheld, EventCallHoldFailed,
		EventCallCompleted, EventCallBilled, EventCallExpired:
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
		err, ev = decodeIfPresent(w.Data, &e.Data), e
	default:
		return &UnknownEvent{eventEnvelope: env, Data: w.Data}, nil
	}
	if err != nil || envErr != nil {
		return nil, invalidPayload()
	}
	return ev, nil
}
