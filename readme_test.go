package yollavoice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// snippetPrelude wraps README fragments (blocks without a package clause) into a function that
// has the usual variables in scope. Every import is used, so a fragment may use any of them.
const snippetPrelude = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	yollavoice "github.com/yolla-io/yolla-voice-go"
)

var (
	_ = errors.New
	_ = fmt.Sprint
	_ = httptest.NewRecorder
	_ = strings.NewReader
	_ = time.Now
	_ = http.StatusOK
	_ = yollavoice.Version
)

func snippet(ctx context.Context, client *yollavoice.Client, rawBody []byte, r *http.Request) {
	_, _, _, _ = ctx, client, rawBody, r
`

var goFence = regexp.MustCompile("(?s)(<!-- not-compiled -->\\s*\n)?```go\n(.*?)```")

type readmeBlock struct {
	line int
	code string
}

// readmeGoBlocks returns the Go blocks of the README that are not marked with a
// not-compiled comment, and the number of skipped blocks.
func readmeGoBlocks(t *testing.T, text string) (blocks []readmeBlock, skipped int) {
	t.Helper()
	for _, m := range goFence.FindAllStringSubmatchIndex(text, -1) {
		if m[2] >= 0 {
			skipped++
			continue
		}
		blocks = append(blocks, readmeBlock{
			line: strings.Count(text[:m[4]], "\n") + 1,
			code: text[m[4]:m[5]],
		})
	}
	return blocks, skipped
}

func TestReadmeBlocksCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("README type-check runs go vet in a temporary module")
	}
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks, skipped := readmeGoBlocks(t, string(raw))
	if len(blocks) < 8 {
		t.Fatalf("found %d Go blocks in README.md, want at least 8", len(blocks))
	}
	if skipped > 1 {
		t.Fatalf("%d README blocks are marked not-compiled, keep it to 1", skipped)
	}
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gomod := fmt.Sprintf("module readmecheck\n\ngo 1.26\n\nrequire github.com/yolla-io/yolla-voice-go v0.0.0\n\nreplace github.com/yolla-io/yolla-voice-go => %s\n", root)
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "go.mod"), gomod)
	for i, b := range blocks {
		src := b.code
		if !strings.HasPrefix(strings.TrimSpace(src), "package ") {
			src = snippetPrelude + src + "}\n\nfunc main() {}\n"
		}
		write(filepath.Join(dir, fmt.Sprintf("b%02d_line%d", i, b.line), "main.go"), src)
	}
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("README Go blocks do not type-check (directory names carry the README line):\n%s", out)
	}
}

func TestReadmeBlockExtraction(t *testing.T) {
	text := "a\n```go\nx := 1\n```\n<!-- not-compiled -->\n```go\ny\n```\n```sh\nz\n```\n"
	blocks, skipped := readmeGoBlocks(t, text)
	if len(blocks) != 1 || skipped != 1 || blocks[0].line != 3 || blocks[0].code != "x := 1\n" {
		t.Fatalf("blocks=%+v skipped=%d", blocks, skipped)
	}
}
