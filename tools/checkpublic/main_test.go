package main

import (
	"reflect"
	"testing"
)

func TestFindForbidden(t *testing.T) {
	cases := []struct {
		name, text, rule string
	}{
		{"host", "see git." + "etser" + "vice" + ".net", "internal host"},
		{"wiki", "https://x/" + "conflu" + "ence/page", "wiki link"},
		{"vendor", "at" + "lassian" + " cloud", "wiki vendor"},
		{"email", "mail me@" + "yolla" + "calls.com", "internal email"},
		{"local", "http://" + "local" + "host:8080/x", "local dev URL"},
		{"cyrillic", "\u043f\u0440\u0438\u0432\u0435\u0442", "Cyrillic text"},
		{"repo path", "see ../" + "platform-" + "api/x", "project repository reference"},
		{"specs", "the " + "platform-" + "specs" + " repo", "project repository reference"},
	}
	for _, c := range cases {
		got := findForbidden(c.text)
		if !reflect.DeepEqual(got, []string{c.rule}) {
			t.Errorf("%s: got %v, want [%s]", c.name, got, c.rule)
		}
	}
}

func TestFindForbiddenClean(t *testing.T) {
	text := "# Yolla Voice for Go\n\nA server SDK for partners. Create calls, check balance, verify webhooks.\n"
	if got := findForbidden(text); len(got) != 0 {
		t.Fatalf("unexpected findings: %v", got)
	}
}
