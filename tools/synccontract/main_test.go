package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `openapi: 3.1.0
info:
  title: T
  version: 1.2.3
  description: prose
tags:
  - name: calls
    description: x
  - name: client
    description: y
security:
  - apiKey: []
paths:
  /v1/calls:
    get:
      operationId: listCalls
      description: d
      tags: [calls]
      responses:
        '200':
          content:
            application/json:
              schema: { $ref: '#/components/schemas/CallList' }
              example: { a: 1 }
        '404': { $ref: '#/components/responses/NotFound' }
        '500':
          description: keep me
  /v1/gateway:
    post:
      operationId: gateway
      x-internal: true
      tags: [client]
      security:
        - callToken: []
      parameters:
        - $ref: '#/components/parameters/Internal'
      requestBody:
        content:
          application/json:
            schema: { $ref: '#/components/schemas/GatewayReq' }
      responses:
        '200': { $ref: '#/components/responses/GatewayOK' }
webhooks:
  call.initiated: { $ref: '#/components/pathItems/CallWebhook' }
components:
  securitySchemes:
    apiKey: { type: http, scheme: bearer, description: k }
    callToken: { type: http, scheme: bearer }
  parameters:
    Internal: { name: x, in: header, schema: { type: string } }
  pathItems:
    CallWebhook:
      post:
        requestBody:
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Event' }
        responses:
          '200': { description: ok }
  responses:
    NotFound:
      content:
        application/json:
          schema: { $ref: '#/components/schemas/Error' }
    GatewayOK:
      description: ok
      content:
        application/json:
          schema: { $ref: '#/components/schemas/GatewayResp' }
  schemas:
    CallList:
      type: object
      example: {}
      properties:
        description: { type: string, description: a field named description }
        item: { $ref: '#/components/schemas/Shared' }
    Event:
      type: object
      x-mvp: true
      properties:
        id: { type: string }
    Error: { type: object }
    Shared: { type: string }
    GatewayReq:
      type: object
      properties:
        s: { $ref: '#/components/schemas/Shared' }
        only: { $ref: '#/components/schemas/OnlyInternal' }
    OnlyInternal: { type: string }
    GatewayResp: { type: object }
`

func mustRender(t *testing.T, src string) string {
	t.Helper()
	out, err := render([]byte(src), "abc1234", "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestStripProseButKeepPropertyNamedDescription(t *testing.T) {
	out := mustRender(t, sample)
	for _, bad := range []string{"prose", "example", "x-mvp", "a field named description", "description: x\n", "description: k"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q", bad)
		}
	}
	if !strings.Contains(out, "description: {type: string}") {
		t.Errorf("property named description was lost:\n%s", out)
	}
}

func TestFillsDescriptionsAndSummaries(t *testing.T) {
	out := mustRender(t, sample)
	for _, want := range []string{"summary: listCalls", "description: Response", "summary: Webhook delivery"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "description: Response") != 4 { // 200, 500 (prose stripped), NotFound, webhook 200
		t.Errorf("expected 4 filled responses, got %d:\n%s", strings.Count(out, "description: Response"), out)
	}
	if strings.Contains(out, "description: keep me") {
		t.Error("explicit description should be stripped as prose")
	}
}

func TestPrunesInternalOperationsAndOrphans(t *testing.T) {
	out := mustRender(t, sample)
	for _, bad := range []string{"/v1/gateway", "callToken", "name: client", "GatewayReq", "GatewayResp", "GatewayOK",
		"OnlyInternal", "Internal:", "x-internal", "parameters:"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"/v1/calls", "name: calls", "apiKey:", "CallList", "Event:", "CallWebhook", "Shared:",
		"NotFound", "Error:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDeterministicAndHeader(t *testing.T) {
	a, b := mustRender(t, sample), mustRender(t, sample)
	if a != b {
		t.Error("output differs between runs")
	}
	first := strings.SplitN(a, "\n", 2)[0]
	want := "# Partner API contract, source version 1.2.3, commit abc1234, synced 2026-01-02. Do not edit; run make sync-contract."
	if first != want {
		t.Errorf("header = %q", first)
	}
	if !strings.HasPrefix(a[len(first)+1:], "openapi: 3.1.0\ninfo:") {
		t.Error("key order not preserved")
	}
}

func TestCyrillicFails(t *testing.T) {
	src := strings.Replace(sample, "enum-placeholder", "", 1) + "  x:\n    Foo: { type: string, title: \u041f\u0440\u0438\u0432\u0435\u0442 }\n"
	if _, err := render([]byte(src), "abc", "2026-01-02"); err == nil || !strings.Contains(err.Error(), "Cyrillic") {
		t.Errorf("expected Cyrillic error, got %v", err)
	}
}

func TestCyrillicInProseIsStripped(t *testing.T) {
	src := strings.Replace(sample, "description: prose", "description: \u041f\u0440\u0438\u0432\u0435\u0442", 1)
	if _, err := render([]byte(src), "abc", "2026-01-02"); err != nil {
		t.Errorf("stripped Cyrillic prose must not fail: %v", err)
	}
}

func TestMainDoesNotWriteOnFailure(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")
	if _, err := render([]byte(strings.Replace(sample, "type: string, description: a field", "type: \u041f\u0440\u0438\u0432\u0435\u0442, description: a field", 1)), "c", "d"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("file exists")
	}
}
