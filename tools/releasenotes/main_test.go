package main

import "testing"

const changelog = `# Changelog

## 0.2.0

Second release.

- Added a thing.

## 0.1.0

Initial release: calls, balance, pricing, webhooks.
`

func TestSection(t *testing.T) {
	cases := []struct {
		version, want string
	}{
		{"0.1.0", "Initial release: calls, balance, pricing, webhooks."},
		{"0.2.0", "Second release.\n\n- Added a thing."},
	}
	for _, c := range cases {
		got, err := section(changelog, c.version)
		if err != nil {
			t.Fatalf("%s: %v", c.version, err)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.version, got, c.want)
		}
	}
}

func TestSectionUnknownVersion(t *testing.T) {
	for _, v := range []string{"0.3.0", "0.1", "0.1.0-rc1", ""} {
		if _, err := section(changelog, v); err == nil {
			t.Errorf("version %q: expected an error", v)
		}
	}
}

func TestSectionEmptyBody(t *testing.T) {
	if _, err := section("## 1.0.0\n\n## 0.9.0\n\nx\n", "1.0.0"); err == nil {
		t.Error("expected an error for an empty section")
	}
}
