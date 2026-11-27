package yollavoice

import (
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func loadContract(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// jsonTags returns the JSON property names of a struct type.
func jsonTags(typ reflect.Type) []string {
	var tags []string
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			tags = append(tags, name)
		}
	}
	sort.Strings(tags)
	return tags
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestCreateCallParamsMatchContract(t *testing.T) {
	doc := loadContract(t)
	schema := doc["components"].(map[string]any)["schemas"].(map[string]any)["CreateCallRequest"].(map[string]any)
	want := sortedKeys(schema["properties"].(map[string]any))
	got := jsonTags(reflect.TypeOf(CreateCallParams{}))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateCallParams JSON tags %v, contract properties %v", got, want)
	}
	required := map[string]bool{}
	for _, r := range schema["required"].([]any) {
		required[r.(string)] = true
	}
	typ := reflect.TypeOf(CreateCallParams{})
	for i := 0; i < typ.NumField(); i++ {
		name, opts, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if required[name] == strings.Contains(opts, "omitempty") {
			t.Errorf("%s: required=%v omitempty=%q", name, required[name], opts)
		}
	}
}

func TestListCallsContract(t *testing.T) {
	doc := loadContract(t)
	op := doc["paths"].(map[string]any)["/v1/calls"].(map[string]any)["get"].(map[string]any)
	var want []string
	for _, p := range op["parameters"].([]any) {
		param := p.(map[string]any)
		if param["in"] == "query" {
			want = append(want, param["name"].(string))
		}
	}
	sort.Strings(want)
	full := ListCallsParams{Status: CallStatusAnswered, ExternalUserID: "u", From: time.Now(), To: time.Now(), Limit: 1, Cursor: "c"}
	got := sortedKeys(toAnyMap(listQuery(full)))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCallsParams sends %v, contract has %v", got, want)
	}
	schema := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	wantProps := sortedKeys(schema["properties"].(map[string]any))
	if gotProps := jsonTags(reflect.TypeOf(listCallsResponse{})); !reflect.DeepEqual(gotProps, wantProps) {
		t.Fatalf("listCallsResponse tags %v, contract properties %v", gotProps, wantProps)
	}
}

func toAnyMap(v url.Values) map[string]any {
	m := map[string]any{}
	for k := range v {
		m[k] = true
	}
	return m
}
