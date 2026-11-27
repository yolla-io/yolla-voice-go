package yollavoice

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestCallDecodesNulls(t *testing.T) {
	var c Call
	readJSON(t, "testdata/call.json", &c)

	if c.ID != "call_01JD7Z3Q8X4M2N6P9R5T1V3W7Y" || c.ExternalUserID != "user-42" {
		t.Fatalf("ID = %q, ExternalUserID = %q", c.ID, c.ExternalUserID)
	}
	if c.Status != CallStatusCreated {
		t.Fatalf("Status = %q", c.Status)
	}
	if c.To != "4915112345678" || c.From != "4930123456" {
		t.Fatalf("To = %q, From = %q", c.To, c.From)
	}
	if want := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC); !c.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %v, want %v", c.CreatedAt, want)
	}
	if want := time.Date(2026, 11, 27, 9, 5, 0, 5e8, time.UTC); !c.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", c.ExpiresAt, want)
	}
	if c.OnHold == nil || *c.OnHold {
		t.Fatalf("OnHold = %v, want pointer to false", c.OnHold)
	}
	if c.MaxDuration == nil || *c.MaxDuration != 3600 {
		t.Fatalf("MaxDuration = %v", c.MaxDuration)
	}
	if c.CustomParams["orderId"] != "A-1001" {
		t.Fatalf("CustomParams = %v", c.CustomParams)
	}
	if c.Rate == nil {
		t.Fatal("Rate is nil")
	}
	if got := c.Rate.PerMinute; got.Amount != "0.012500" || got.Currency != "EUR" {
		t.Fatalf("PerMinute = %+v, want string money 0.012500 EUR", got)
	}
	if c.Rate.FirstIncrementSec != 60 || c.Rate.NextIncrementSec != 1 || c.Rate.Prefix != "4915" {
		t.Fatalf("Rate = %+v", *c.Rate)
	}

	nulls := map[string]bool{
		"InitiatedAt":    c.InitiatedAt == nil,
		"AnsweredAt":     c.AnsweredAt == nil,
		"EndedAt":        c.EndedAt == nil,
		"Duration":       c.Duration == nil,
		"BilledDuration": c.BilledDuration == nil,
		"Price":          c.Price == nil,
		"EndReason":      c.EndReason == nil,
		"SIPCode":        c.SIPCode == nil,
		"ErrorCode":      c.ErrorCode == nil,
		"Quality":        c.Quality == nil,
	}
	for name, isNil := range nulls {
		if !isNil {
			t.Errorf("%s is not nil for a JSON null", name)
		}
	}
}

func TestCallKeepsUnknownStatus(t *testing.T) {
	var c Call
	readJSON(t, "testdata/call_unknown.json", &c)

	if c.Status != CallStatus("transferred") {
		t.Fatalf("Status = %q, want the unknown value kept", c.Status)
	}
	if c.Price == nil || c.Price.Amount != "0.25" || c.Price.Currency != "EUR" {
		t.Fatalf("Price = %+v", c.Price)
	}
	if c.EndReason == nil || *c.EndReason != EndReasonCalleeHangup {
		t.Fatalf("EndReason = %v", c.EndReason)
	}
	if c.OnHold != nil || c.Rate != nil {
		t.Fatal("absent optional fields are not nil")
	}
}

// contractSchema returns a component schema from the contract snapshot.
func contractSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	var doc struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	readJSON(t, "testdata/contract.json", &doc)
	s, ok := doc.Components.Schemas[name]
	if !ok {
		t.Fatalf("schema %s is not in the contract", name)
	}
	return s
}

func contractEnum(t *testing.T, name string) []string {
	t.Helper()
	var values []string
	for _, v := range contractSchema(t, name)["enum"].([]any) {
		values = append(values, v.(string))
	}
	slices.Sort(values)
	return values
}

func TestCallStatusConstantsMatchContract(t *testing.T) {
	consts := []CallStatus{
		CallStatusCreated, CallStatusInitiated, CallStatusAnswered, CallStatusCompleted,
		CallStatusFailed, CallStatusCanceled, CallStatusExpired,
	}
	var got []string
	for _, c := range consts {
		got = append(got, string(c))
	}
	slices.Sort(got)
	if want := contractEnum(t, "CallStatus"); !slices.Equal(got, want) {
		t.Fatalf("CallStatus constants = %v, contract = %v", got, want)
	}
}

func TestEndReasonConstantsMatchContract(t *testing.T) {
	consts := []EndReason{
		EndReasonCallerHangup, EndReasonCalleeHangup, EndReasonCallerCanceled, EndReasonIntentCanceled,
		EndReasonMediaTimeout, EndReasonConnectionLost, EndReasonMaxDuration, EndReasonBalanceExhausted,
		EndReasonServerTerminated, EndReasonBusy, EndReasonNoAnswer, EndReasonRejected, EndReasonRingTimeout,
		EndReasonCarrierFailed, EndReasonPlatformRejected, EndReasonUnknown,
	}
	var got []string
	for _, c := range consts {
		got = append(got, string(c))
	}
	slices.Sort(got)
	if want := contractEnum(t, "EndReason"); !slices.Equal(got, want) {
		t.Fatalf("EndReason constants = %v, contract = %v", got, want)
	}
}

func TestGeneratedTypesImportOnlyStdlib(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "types.gen.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
			t.Errorf("types.gen.go imports %q, outside the standard library", path)
		}
	}
}
