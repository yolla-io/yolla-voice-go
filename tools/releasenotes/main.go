// Command releasenotes prints the CHANGELOG.md section of one version, for
// release notes. Usage: releasenotes <version> [repo-root]; a leading "v" in
// the version is ignored and the repo root defaults to the current directory.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// section returns the text under the "## <version>" heading up to the next
// "## " heading, trimmed. It fails for an unknown version or an empty section.
func section(changelog, version string) (string, error) {
	if !versionRe.MatchString(version) {
		return "", fmt.Errorf("invalid version %q", version)
	}
	var body []string
	found := false
	for _, line := range strings.Split(changelog, "\n") {
		if strings.HasPrefix(line, "## ") {
			if found {
				break
			}
			found = strings.TrimSpace(line[3:]) == version
			continue
		}
		if found {
			body = append(body, line)
		}
	}
	if !found {
		return "", fmt.Errorf("version %s not found in CHANGELOG.md", version)
	}
	text := strings.TrimSpace(strings.Join(body, "\n"))
	if text == "" {
		return "", fmt.Errorf("CHANGELOG.md section for %s is empty", version)
	}
	return text, nil
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: releasenotes <version> [repo-root]")
		os.Exit(2)
	}
	root := "."
	if len(os.Args) == 3 {
		root = os.Args[2]
	}
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	text, err := section(string(data), strings.TrimPrefix(os.Args[1], "v"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(text)
}
