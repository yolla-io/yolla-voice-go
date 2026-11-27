package yollavoice

import (
	"regexp"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version != "0.2.0" {
		t.Fatalf("Version = %q, want 0.2.0", Version)
	}
	semver := regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	if !semver.MatchString(Version) {
		t.Fatalf("Version %q is not valid semver without a v prefix", Version)
	}
}
