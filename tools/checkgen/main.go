// Command checkgen fails when the committed generated files differ from a
// fresh run of gen. Usage, from the tools directory: checkgen <repository root>.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// outputs are the files gen writes, relative to the repository root.
var outputs = []string{"types.gen.go", "testdata/contract.json"}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: checkgen <repository root>")
		os.Exit(2)
	}
	stale, err := check(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "checkgen:", err)
		os.Exit(1)
	}
	if len(stale) > 0 {
		for _, name := range stale {
			fmt.Fprintln(os.Stderr, "differs:", name)
		}
		fmt.Fprintln(os.Stderr, "Generated types are stale: run make gen")
		os.Exit(1)
	}
}

// check generates into a temporary directory and returns the outputs that
// differ from the committed files.
func check(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "yollavoice-checkgen-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	cmd := exec.Command("go", "run", "./gen", root, tmp)
	cmd.Dir = filepath.Join(root, "tools")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gen: %w", err)
	}

	var stale []string
	for _, name := range outputs {
		want, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			return nil, err
		}
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			stale = append(stale, name)
		}
	}
	return stale, nil
}
