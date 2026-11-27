// Command checkdeps fails when the root go.mod has any require directive: the
// SDK has no dependencies. Usage: checkdeps [repo-root] (default: current directory).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var requireRe = regexp.MustCompile(`(?m)^\s*require\b`)

// hasRequire reports whether go.mod text contains a require directive.
func hasRequire(text string) bool {
	return requireRe.MatchString(text)
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if hasRequire(string(data)) {
		fmt.Fprintln(os.Stderr, "go.mod: require directive found; the SDK must have no dependencies")
		os.Exit(1)
	}
	fmt.Println("Dependency guard: go.mod has no require lines")
}
