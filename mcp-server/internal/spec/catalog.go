package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Source describes one named documentation corpus.
type Source struct {
	Name    string
	Roots   []string
	Indexes []string
	Include []string
	Exclude []string
}

// LoadOptions bounds corpus ingestion.
type LoadOptions struct {
	MaxFiles     int
	MaxFileBytes int64
}

// MatchInput describes the current development or browser context.
type MatchInput struct {
	File      string
	Component string
	Route     string
	Selector  string
	Terms     []string
	Max       int
}

// Match is compact, ranked spec context suitable for an agent response.
type Match struct {
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	Path      string    `json:"path"`
	Corpus    string    `json:"corpus"`
	Summary   string    `json:"summary,omitempty"`
	ReadWhen  string    `json:"read_when,omitempty"`
	DocType   string    `json:"doc_type,omitempty"`
	Subsystem string    `json:"subsystem,omitempty"`
	Bindings  []Binding `json:"bindings,omitempty"`
	Score     int       `json:"score"`
	Excerpt   string    `json:"excerpt,omitempty"`
}

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)#?]+\.md)(?:#[^)]+)?\)`)

// LoadSources loads generic Markdown specs and BrowserNERD executable
// invariants from named, bounded sources. Indexes prioritize linked documents;
// each root is still scanned so stale or incomplete indexes cannot hide specs.
func LoadSources(sources []Source, opts LoadOptions) ([]Spec, []error) {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 2000
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = 2 << 20
	}

	var (
		specs []Spec
		errs  []error
		seen  = make(map[string]bool)
	)
	for _, source := range sources {
		paths, sourceErrs := sourcePaths(source, opts.MaxFiles-len(specs))
		errs = append(errs, sourceErrs...)
		for _, path := range paths {
			if len(specs) >= opts.MaxFiles {
				errs = append(errs, fmt.Errorf("spec file limit reached (%d)", opts.MaxFiles))
				return specs, errs
			}
			canonical, err := filepath.Abs(path)
			if err != nil || seen[canonical] {
				continue
			}
			seen[canonical] = true

			info, err := os.Stat(canonical)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", canonical, err))
				continue
			}
			if info.IsDir() || info.Size() > opts.MaxFileBytes {
				if info.Size() > opts.MaxFileBytes {
					errs = append(errs, fmt.Errorf("%s exceeds max_file_bytes", canonical))
				}
				continue
			}
			content, err := os.ReadFile(canonical)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", canonical, err))
				continue
			}
			doc, err := Parse(canonical, content)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			doc.Corpus = source.Name
			specs = append(specs, doc)
		}
	}
	return specs, errs
}

func sourcePaths(source Source, remaining int) ([]string, []error) {
	if remaining <= 0 {
		return nil, nil
	}
	var (
		paths []string
		errs  []error
		seen  = make(map[string]bool)
	)
	appendPath := func(path string) {
		if len(paths) >= remaining || !strings.EqualFold(filepath.Ext(path), ".md") {
			return
		}
		clean := filepath.Clean(path)
		if seen[clean] || !matchesSourcePatterns(clean, source) {
			return
		}
		seen[clean] = true
		paths = append(paths, clean)
	}

	for _, indexPath := range source.Indexes {
		content, err := os.ReadFile(indexPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("read spec index %s: %w", indexPath, err))
			continue
		}
		indexDir := filepath.Dir(indexPath)
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(content), -1) {
			if len(match) > 1 {
				appendPath(filepath.Join(indexDir, filepath.FromSlash(match[1])))
			}
		}
	}

	for _, root := range source.Roots {
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if path != root && excludedPath(path, source.Exclude) {
					return filepath.SkipDir
				}
				return nil
			}
			appendPath(path)
			return nil
		})
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("scan spec root %s: %w", root, walkErr))
		}
	}
	return paths, errs
}

func matchesSourcePatterns(path string, source Source) bool {
	normalized := filepath.ToSlash(path)
	if excludedPath(normalized, source.Exclude) {
		return false
	}
	if len(source.Include) == 0 {
		return true
	}
	for _, pattern := range source.Include {
		if wildcardMatch(filepath.ToSlash(pattern), normalized) {
			return true
		}
	}
	return false
}

func excludedPath(path string, patterns []string) bool {
	normalized := filepath.ToSlash(path)
	for _, pattern := range patterns {
		if wildcardMatch(filepath.ToSlash(pattern), normalized) {
			return true
		}
	}
	return false
}

func wildcardMatch(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if ok, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(value)); ok {
		return true
	}
	needle := strings.Trim(pattern, "*")
	return strings.HasPrefix(pattern, "**/") && strings.Contains(value, strings.TrimPrefix(needle, "/")) ||
		strings.HasSuffix(pattern, "/**") && strings.Contains(value, strings.TrimSuffix(needle, "/"))
}

// MatchSpecs ranks generic documents and executable specs against context.
func MatchSpecs(specs []Spec, input MatchInput, maxExcerptBytes int) []Match {
	if input.Max <= 0 {
		input.Max = 12
	}
	if maxExcerptBytes <= 0 {
		maxExcerptBytes = 1200
	}
	terms := normalizedTerms(input)
	type ranked struct {
		spec  Spec
		score int
	}
	var matches []ranked
	for _, doc := range specs {
		score := specScore(doc, input, terms)
		if score <= 0 && (input.File != "" || input.Component != "" || input.Route != "" || input.Selector != "" || len(input.Terms) > 0) {
			continue
		}
		matches = append(matches, ranked{spec: doc, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].spec.Path < matches[j].spec.Path
	})
	if len(matches) > input.Max {
		matches = matches[:input.Max]
	}
	out := make([]Match, 0, len(matches))
	for _, item := range matches {
		doc := item.spec
		out = append(out, Match{
			Name:      doc.Name,
			Title:     doc.Title,
			Path:      doc.Path,
			Corpus:    doc.Corpus,
			Summary:   truncateText(doc.Summary, maxExcerptBytes/2),
			ReadWhen:  doc.ReadWhen,
			DocType:   doc.DocType,
			Subsystem: doc.Subsystem,
			Bindings:  doc.Bindings,
			Score:     item.score,
			Excerpt:   truncateText(relevantExcerpt(doc.Body, terms), maxExcerptBytes),
		})
	}
	return out
}

func normalizedTerms(input MatchInput) []string {
	values := append([]string{}, input.Terms...)
	values = append(values, input.File, input.Component, input.Route, input.Selector)
	seen := make(map[string]bool)
	var terms []string
	for _, value := range values {
		for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(term) >= 3 && !seen[term] {
				seen[term] = true
				terms = append(terms, term)
			}
		}
	}
	return terms
}

func specScore(doc Spec, input MatchInput, terms []string) int {
	score := 0
	for _, binding := range doc.Bindings {
		switch strings.ToLower(binding.Kind) {
		case "component":
			if input.Component != "" && strings.EqualFold(binding.Target, input.Component) {
				score += 100
			}
		case "route":
			if input.Route != "" && routeMatches(binding.Target, input.Route) {
				score += 100
			}
		case "selector":
			if input.Selector != "" && binding.Target == input.Selector {
				score += 100
			}
		}
	}
	if input.File != "" && (sameFile(doc.Source, input.File) || sameFile(doc.Path, input.File)) {
		score += 100
	}
	haystack := strings.ToLower(strings.Join([]string{
		doc.Name, doc.Title, doc.Summary, doc.ReadWhen, doc.DocType,
		doc.Subsystem, strings.Join(doc.Tags, " "), doc.Path, doc.Body,
	}, " "))
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			score += 5
		}
	}
	return score
}

func routeMatches(binding, route string) bool {
	binding = strings.TrimSuffix(strings.TrimSpace(binding), "/")
	route = strings.TrimSuffix(strings.TrimSpace(route), "/")
	return binding == route || strings.HasPrefix(route, binding+"/")
}

func relevantExcerpt(body string, terms []string) string {
	body = strings.TrimSpace(body)
	if body == "" || len(terms) == 0 {
		return body
	}
	lower := strings.ToLower(body)
	best := -1
	for _, term := range terms {
		if idx := strings.Index(lower, term); idx >= 0 && (best == -1 || idx < best) {
			best = idx
		}
	}
	if best <= 0 {
		return body
	}
	start := best - 240
	if start < 0 {
		start = 0
	}
	return body[start:]
}

func truncateText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
