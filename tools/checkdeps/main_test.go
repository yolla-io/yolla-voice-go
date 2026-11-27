package main

import "testing"

func TestHasRequire(t *testing.T) {
	cases := []struct {
		name, text string
		want       bool
	}{
		{"bootstrap", "module github.com/yolla-io/yolla-voice-go\n\ngo 1.26\n", false},
		{"single", "module m\n\ngo 1.26\n\nrequire example.com/x v1.0.0\n", true},
		{"block", "module m\n\ngo 1.26\n\nrequire (\n\texample.com/x v1.0.0\n)\n", true},
		{"comment", "module m\n// require example.com/x v1.0.0\n", false},
	}
	for _, c := range cases {
		if got := hasRequire(c.text); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
