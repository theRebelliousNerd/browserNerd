// Package spec parses frontend specification documents: Markdown files with a
// YAML frontmatter block and inline invariant tags. Invariants compile to
// Mangle queries so the engine can both deliver them alongside browser work
// (by component, route, or code line-range) and check them for violations.
package spec

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Binding ties a spec to something observable on the page.
type Binding struct {
	Kind   string `yaml:"kind" json:"kind"`     // component | route | selector
	Target string `yaml:"target" json:"target"` // component name, route path, or selector
}

// Invariant is a single checkable condition. Query is a Mangle query scored by
// Expect ("present" or "absent"). When it governs a code region, File/From/To
// locate it so an agent editing those lines receives it.
type Invariant struct {
	Name   string `json:"name"`
	Query  string `json:"query"`
	Expect string `json:"expect"` // "present" (default) | "absent"

	Prose  string `json:"prose,omitempty"`
	File   string `json:"file,omitempty"` // source file this invariant governs
	From   int    `json:"from,omitempty"` // 1-based start line, 0 if unset
	To     int    `json:"to,omitempty"`   // inclusive end line, 0 if unset
	Inline bool   `json:"inline"`         // inline body tag vs frontmatter
}

// Covers reports whether the invariant governs any line in [from,to] of file.
// A zero from/to on either side means "no range", which never overlaps.
func (i Invariant) Covers(file string, from, to int) bool {
	if i.File == "" || file == "" || i.From == 0 || i.To == 0 || from == 0 || to == 0 {
		return false
	}
	if !sameFile(i.File, file) {
		return false
	}
	return i.From <= to && from <= i.To
}

// InFile reports whether the invariant governs the given file, ignoring line
// ranges. Used for whole-file spec delivery.
func (i Invariant) InFile(file string) bool {
	return i.File != "" && file != "" && sameFile(i.File, file)
}

// Spec is one parsed specification document.
type Spec struct {
	Name       string      `json:"name"`
	Title      string      `json:"title,omitempty"`
	Path       string      `json:"path"`
	Corpus     string      `json:"corpus,omitempty"`
	Source     string      `json:"source,omitempty"`
	Summary    string      `json:"summary,omitempty"`
	ReadWhen   string      `json:"read_when,omitempty"`
	DocType    string      `json:"doc_type,omitempty"`
	Subsystem  string      `json:"subsystem,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	Bindings   []Binding   `json:"bindings,omitempty"`
	Invariants []Invariant `json:"invariants"`
	Body       string      `json:"-"`
}

// frontmatter is the YAML block at the top of a spec document.
type frontmatter struct {
	Name        string    `yaml:"name"`
	Title       string    `yaml:"title"`
	Summary     string    `yaml:"summary"`
	Description string    `yaml:"description"`
	ReadWhen    string    `yaml:"read_when"`
	DocType     string    `yaml:"doc_type"`
	Subsystem   string    `yaml:"subsystem"`
	Tags        []string  `yaml:"tags"`
	Source      string    `yaml:"source"`
	Binding     []Binding `yaml:"binding"`
	Invariants  []struct {
		Name   string `yaml:"name"`
		Query  string `yaml:"query"`
		Expect string `yaml:"expect"`
	} `yaml:"invariants"`
}

const (
	invariantOpenPrefix = "browsernerd:invariant"
	invariantClose      = "browsernerd:end"
)

// Parse parses a spec document. path is used for error messages and to resolve
// relative source paths; content is the raw file bytes.
func Parse(path string, content []byte) (Spec, error) {
	fm, body, bodyOffset, err := splitFrontmatter(content)
	if err != nil {
		return Spec{}, fmt.Errorf("%s: %w", path, err)
	}

	spec := Spec{
		Name:      fm.Name,
		Title:     stripMarkdownTitle(fm.Title),
		Path:      path,
		Source:    fm.Source,
		Summary:   firstNonEmpty(fm.Summary, fm.Description),
		ReadWhen:  fm.ReadWhen,
		DocType:   fm.DocType,
		Subsystem: fm.Subsystem,
		Tags:      append([]string(nil), fm.Tags...),
		Bindings:  fm.Binding,
		Body:      body,
	}
	if spec.Name == "" {
		spec.Name = firstNonEmpty(spec.Title, firstMarkdownHeading(body), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}
	if spec.Title == "" {
		spec.Title = firstNonEmpty(firstMarkdownHeading(body), spec.Name)
	}
	if spec.Summary == "" {
		spec.Summary = firstParagraph(body)
	}

	// Frontmatter-declared invariants apply spec-wide.
	for _, inv := range fm.Invariants {
		spec.Invariants = append(spec.Invariants, Invariant{
			Name:   inv.Name,
			Query:  inv.Query,
			Expect: inv.Expect,
			File:   spec.Source,
		})
	}

	inline, err := parseInlineInvariants(body, bodyOffset, spec.Source)
	if err != nil {
		return Spec{}, fmt.Errorf("%s: %w", path, err)
	}
	spec.Invariants = append(spec.Invariants, inline...)

	return spec, nil
}

func stripMarkdownTitle(value string) string {
	return strings.Trim(strings.TrimSpace(value), "*_")
}

func firstMarkdownHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return stripMarkdownTitle(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")))
		}
	}
	return ""
}

func firstParagraph(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "<!--") || strings.HasPrefix(trimmed, "```") {
			continue
		}
		lines = append(lines, trimmed)
		if len(strings.Join(lines, " ")) >= 320 {
			break
		}
	}
	return strings.Join(lines, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// splitFrontmatter separates a leading `---`-delimited YAML block from the body.
// It returns the parsed frontmatter, the body text, and the 1-based line number
// on which the body begins (so inline from/to can be reported doc-relative).
func splitFrontmatter(content []byte) (frontmatter, string, int, error) {
	text := string(content)
	var fm frontmatter

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fm, "", 0, err
	}

	// No frontmatter: whole document is body starting at line 1.
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, text, 1, nil
	}

	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return fm, "", 0, fmt.Errorf("unterminated frontmatter block")
	}

	yamlText := strings.Join(lines[1:closeIdx], "\n")
	if err := yaml.Unmarshal([]byte(yamlText), &fm); err != nil {
		return fm, "", 0, fmt.Errorf("parse frontmatter: %w", err)
	}

	body := strings.Join(lines[closeIdx+1:], "\n")
	bodyOffset := closeIdx + 2 // 1-based line of the first body line
	return fm, body, bodyOffset, nil
}

// parseInlineInvariants extracts <!-- browsernerd:invariant ... --> ...
// <!-- browsernerd:end --> blocks from the body. bodyOffset is the doc-relative
// line number of the body's first line. defaultSource is the spec's source file,
// used when an invariant does not override it with `in:`.
func parseInlineInvariants(body string, bodyOffset int, defaultSource string) ([]Invariant, error) {
	lines := strings.Split(body, "\n")
	invariants := make([]Invariant, 0)

	i := 0
	for i < len(lines) {
		attrs, ok := matchDirective(lines[i], invariantOpenPrefix)
		if !ok {
			i++
			continue
		}

		openLine := bodyOffset + i // doc-relative line of the opening tag
		inv := Invariant{
			Inline: true,
			Name:   attrs["name"],
			Expect: attrs["expect"],
			File:   defaultSource,
		}
		if in := attrs["in"]; in != "" {
			inv.File = in
		}
		inv.From = atoiSafe(attrs["from"])
		inv.To = atoiSafe(attrs["to"])

		// Collect the block until the close tag.
		var prose []string
		var query []string
		inQuery := false
		closed := false
		j := i + 1
		for ; j < len(lines); j++ {
			if _, ok := matchDirective(lines[j], invariantClose); ok {
				closed = true
				break
			}
			line := lines[j]
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				// Toggle a fenced block; a ```query fence carries the Mangle query.
				if !inQuery && strings.HasPrefix(trimmed, "```query") {
					inQuery = true
				} else if inQuery {
					inQuery = false
				}
				continue
			}
			if inQuery {
				query = append(query, line)
			} else {
				prose = append(prose, line)
			}
		}
		if !closed {
			return nil, fmt.Errorf("invariant %q opened at line %d has no browsernerd:end", inv.Name, openLine)
		}

		inv.Query = strings.TrimSpace(strings.Join(query, "\n"))
		inv.Prose = strings.TrimSpace(strings.Join(prose, "\n"))
		if inv.Name == "" {
			inv.Name = fmt.Sprintf("invariant_line_%d", openLine)
		}
		invariants = append(invariants, inv)

		i = j + 1
	}

	return invariants, nil
}

// matchDirective matches an HTML-comment directive line of the form
// `<!-- prefix key=value key:value ... -->` and returns its attributes.
func matchDirective(line, prefix string) (map[string]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "<!--") || !strings.HasSuffix(trimmed, "-->") {
		return nil, false
	}
	inner := strings.TrimSpace(trimmed[len("<!--") : len(trimmed)-len("-->")])
	if !strings.HasPrefix(inner, prefix) {
		return nil, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(inner, prefix))

	attrs := make(map[string]string)
	for _, tok := range strings.Fields(rest) {
		key, val := splitAttr(tok)
		if key != "" {
			attrs[key] = val
		}
	}
	return attrs, true
}

// splitAttr splits a `key=value` or `key:value` token. Bare tokens become
// key with an empty value.
func splitAttr(tok string) (string, string) {
	for _, sep := range []string{"=", ":"} {
		if idx := strings.Index(tok, sep); idx >= 0 {
			return tok[:idx], tok[idx+1:]
		}
	}
	return tok, ""
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func sameFile(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b) || filepath.Base(a) == filepath.Base(b)
}

// LoadDir parses every spec document under dir (recursively). It reads files
// with a .md or .spec.md extension. Parse errors are collected per-file and
// returned alongside the specs that did parse.
func LoadDir(dir string) ([]Spec, []error) {
	var specs []Spec
	var errs []error

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, readErr))
			return nil
		}
		s, parseErr := Parse(path, content)
		if parseErr != nil {
			errs = append(errs, parseErr)
			return nil
		}
		specs = append(specs, s)
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return specs, errs
}
