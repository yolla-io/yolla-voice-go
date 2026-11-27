// Command synccontract writes openapi/partner-api.yaml from a source OpenAPI
// contract: prose and examples are stripped, internal operations and the
// components only they use are pruned, and a version header is prepended.
// Usage: synccontract <source openapi.yaml> <output file>.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// stripKeys are prose, examples and planning metadata. They are dropped wherever
// they appear, except as keys of `properties`, where a key is a field name.
var stripKeys = map[string]bool{
	"description": true, "summary": true, "example": true, "examples": true, "x-mvp": true,
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: synccontract <source openapi.yaml> <output file>")
		os.Exit(2)
	}
	src, out := os.Args[1], os.Args[2]
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res, err := render(data, sourceCommit(src), time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, res, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// render converts the source contract into the snapshot text.
func render(src []byte, commit, date string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("source is not an OpenAPI mapping document")
	}
	doc := root.Content[0]
	prune(doc)
	strip(doc, "")
	if c := get(doc, "components"); c != nil {
		if r := get(c, "responses"); r != nil {
			fillResponses(r)
		}
	}
	version := ""
	if info := get(doc, "info"); info != nil {
		if v := get(info, "version"); v != nil {
			version = v.Value
		}
	}
	if version == "" {
		return nil, errors.New("info.version is missing")
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	body := buf.String()
	if loc := cyrillic.FindStringIndex(body); loc != nil {
		line := body[strings.LastIndex(body[:loc[0]], "\n")+1:]
		if i := strings.Index(line, "\n"); i >= 0 {
			line = line[:i]
		}
		return nil, fmt.Errorf("Cyrillic text remains in the contract: %s", strings.TrimSpace(line))
	}
	header := fmt.Sprintf("# Partner API contract, source version %s, commit %s, synced %s. Do not edit; run make sync-contract.\n",
		version, commit, date)
	return []byte(header + body), nil
}

// get returns the value of key in a mapping node, or nil.
func get(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func del(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func set(m *yaml.Node, key, value string) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func strip(n *yaml.Node, parentKey string) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			strip(c, "")
		}
	case yaml.MappingNode:
		kept := n.Content[:0:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if parentKey != "properties" && stripKeys[k.Value] {
				continue
			}
			strip(v, k.Value)
			kept = append(kept, k, v)
		}
		n.Content = kept
		if r := get(n, "responses"); r != nil {
			fillResponses(r)
		}
		if op := get(n, "operationId"); op != nil && get(n, "summary") == nil {
			set(n, "summary", op.Value)
		}
		if post := get(n, "post"); post != nil && post.Kind == yaml.MappingNode &&
			get(post, "responses") != nil && get(post, "summary") == nil {
			set(post, "summary", "Webhook delivery")
		}
	case yaml.AliasNode:
		if n.Alias != nil {
			strip(n.Alias, parentKey)
		}
	}
}

// fillResponses gives every inline response object a description, which OpenAPI requires.
func fillResponses(responses *yaml.Node) {
	if responses.Kind != yaml.MappingNode {
		return
	}
	for i := 1; i < len(responses.Content); i += 2 {
		r := responses.Content[i]
		if r.Kind == yaml.MappingNode && get(r, "$ref") == nil && get(r, "description") == nil {
			set(r, "description", "Response")
		}
	}
}

// refs holds what a part of the document points at.
type refs struct {
	comps map[string]bool // "kind/name"
	tags  map[string]bool
}

func newRefs() *refs { return &refs{comps: map[string]bool{}, tags: map[string]bool{}} }

// collect gathers component references ($ref and security requirements) and tags below n.
func (r *refs) collect(n *yaml.Node, parentKey string) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			r.collect(c, "")
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			switch {
			case k.Value == "$ref" && v.Kind == yaml.ScalarNode && parentKey != "properties":
				if c, ok := componentOf(v.Value); ok {
					r.comps[c] = true
				}
			case k.Value == "security" && v.Kind == yaml.SequenceNode && parentKey != "properties":
				for _, req := range v.Content {
					if req.Kind == yaml.MappingNode {
						for j := 0; j < len(req.Content); j += 2 {
							r.comps["securitySchemes/"+req.Content[j].Value] = true
						}
					}
				}
			case k.Value == "tags" && v.Kind == yaml.SequenceNode && parentKey != "properties":
				for _, t := range v.Content {
					r.tags[t.Value] = true
				}
			}
			r.collect(v, k.Value)
		}
	case yaml.AliasNode:
		if n.Alias != nil {
			r.collect(n.Alias, parentKey)
		}
	}
}

func componentOf(ref string) (string, bool) {
	rest, ok := strings.CutPrefix(ref, "#/components/")
	if !ok {
		return "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// prune removes x-internal operations, the paths and tags left empty, and the
// components that only the removed operations used (transitively).
func prune(doc *yaml.Node) {
	removed := newRefs()
	if paths := get(doc, "paths"); paths != nil && paths.Kind == yaml.MappingNode {
		keep := paths.Content[:0:0]
		for i := 0; i+1 < len(paths.Content); i += 2 {
			item := paths.Content[i+1]
			ops := 0
			if item.Kind == yaml.MappingNode {
				kept := item.Content[:0:0]
				for j := 0; j+1 < len(item.Content); j += 2 {
					k, v := item.Content[j], item.Content[j+1]
					if httpMethods[k.Value] {
						if x := get(v, "x-internal"); x != nil && x.Value == "true" {
							removed.collect(v, "")
							continue
						}
						ops++
					}
					kept = append(kept, k, v)
				}
				item.Content = kept
			}
			if ops == 0 {
				continue
			}
			keep = append(keep, paths.Content[i], item)
		}
		paths.Content = keep
	}

	comps := get(doc, "components")
	var all map[string]*yaml.Node // "kind/name" -> node
	if comps != nil && comps.Kind == yaml.MappingNode {
		all = map[string]*yaml.Node{}
		for i := 0; i+1 < len(comps.Content); i += 2 {
			kind, m := comps.Content[i].Value, comps.Content[i+1]
			for j := 0; j+1 < len(m.Content); j += 2 {
				all[kind+"/"+m.Content[j].Value] = m.Content[j+1]
			}
		}
	}

	// Candidates: everything the removed operations reach.
	candidates := closure(all, removed.comps)
	// Kept: everything reachable from the document outside components and from
	// components that are not candidates.
	rootRefs := newRefs()
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "components" {
			rootRefs.collect(doc.Content[i+1], doc.Content[i].Value)
		}
	}
	seeds := rootRefs.comps
	for name := range all {
		if !candidates[name] {
			seeds[name] = true
		}
	}
	reachable := closure(all, seeds)

	if comps != nil && comps.Kind == yaml.MappingNode {
		keepKinds := comps.Content[:0:0]
		for i := 0; i+1 < len(comps.Content); i += 2 {
			kind, m := comps.Content[i], comps.Content[i+1]
			kept := m.Content[:0:0]
			for j := 0; j+1 < len(m.Content); j += 2 {
				name := kind.Value + "/" + m.Content[j].Value
				if candidates[name] && !reachable[name] {
					continue
				}
				kept = append(kept, m.Content[j], m.Content[j+1])
			}
			if len(kept) == 0 {
				continue
			}
			m.Content = kept
			keepKinds = append(keepKinds, kind, m)
		}
		comps.Content = keepKinds
	}

	// Tags used only by removed operations.
	used := newRefs()
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if k := doc.Content[i].Value; k != "tags" && k != "components" {
			used.collect(doc.Content[i+1], k)
		}
	}
	if c := get(doc, "components"); c != nil {
		used.collect(c, "")
	}
	if tags := get(doc, "tags"); tags != nil && tags.Kind == yaml.SequenceNode {
		kept := tags.Content[:0:0]
		for _, t := range tags.Content {
			if name := get(t, "name"); name != nil && removed.tags[name.Value] && !used.tags[name.Value] {
				continue
			}
			kept = append(kept, t)
		}
		tags.Content = kept
	}
}

// closure returns the seeds plus every component reachable from them.
func closure(all map[string]*yaml.Node, seeds map[string]bool) map[string]bool {
	out := map[string]bool{}
	var queue []string
	for s := range seeds {
		out[s] = true
		queue = append(queue, s)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		n, ok := all[name]
		if !ok {
			continue
		}
		r := newRefs()
		r.collect(n, "")
		for c := range r.comps {
			if !out[c] {
				out[c] = true
				queue = append(queue, c)
			}
		}
	}
	return out
}

// sourceCommit is the last commit that changed the source file, with "+changes"
// when the file is modified, or "unknown" on any git error.
func sourceCommit(file string) string {
	dir, base := filepath.Dir(file), filepath.Base(file)
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	hash, err := git("log", "-1", "--format=%h", "--", base)
	if err != nil || hash == "" {
		return "unknown"
	}
	status, err := git("status", "--porcelain", "--", base)
	if err != nil {
		return "unknown"
	}
	if status != "" {
		return hash + "+changes"
	}
	return hash
}
