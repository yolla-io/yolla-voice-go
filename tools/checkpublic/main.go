// Command checkpublic is the public-repository guard: it fails when a tracked
// file holds internal or non-English text. The repository is mirrored to a
// public host. Usage: checkpublic [repo-root] (default: current directory).
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

type rule struct {
	name string
	re   *regexp.Regexp
}

var rules = []rule{
	{"internal host", regexp.MustCompile(`(?i)etservice`)},
	{"wiki link", regexp.MustCompile(`(?i)confluence`)},
	{"wiki vendor", regexp.MustCompile(`(?i)atlassian`)},
	{"internal email", regexp.MustCompile(`(?i)@yollacalls\.com`)},
	{"local dev URL", regexp.MustCompile(`localhost:8080`)},
	{"Cyrillic text", regexp.MustCompile(`\p{Cyrillic}`)},
	{"project repository reference", regexp.MustCompile(`\.\./platform-|platform-specs`)},
}

// ownSource is skipped because it holds the patterns themselves.
const ownSource = "tools/checkpublic/main.go"

// findForbidden returns the names of the rules that match text.
func findForbidden(text string) []string {
	var out []string
	for _, r := range rules {
		if r.re.MatchString(text) {
			out = append(out, r.name)
		}
	}
	return out
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	listing, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "git ls-files failed:", err)
		os.Exit(2)
	}
	failed, count := false, 0
	for _, f := range bytes.Split(listing, []byte{0}) {
		name := string(f)
		if name == "" || name == ownSource {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in the working tree
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		count++
		for _, r := range findForbidden(string(data)) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", name, r)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("Public-repo guard: %d tracked files clean\n", count)
}
