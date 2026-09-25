package mcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/spec"
)

const defaultSpecsDir = ".browsernerd/specs"

// specFilter scopes which invariants a spec tool operates on.
type specFilter struct {
	file      string
	from, to  int
	component string
	route     string
	selector  string
}

func (f specFilter) hasBinding() bool {
	return f.component != "" || f.route != "" || f.selector != ""
}

func (f specFilter) hasFile() bool { return f.file != "" }

func (f specFilter) empty() bool { return !f.hasBinding() && !f.hasFile() }

func specFilterFromArgs(args map[string]interface{}) specFilter {
	f := specFilter{
		file:      getStringArg(args, "file"),
		component: getStringArg(args, "component"),
		route:     getStringArg(args, "route"),
		selector:  getStringArg(args, "selector"),
	}
	f.from = getIntArg(args, "from", 0)
	f.to = getIntArg(args, "to", 0)
	if line := getIntArg(args, "line", 0); line > 0 && f.from == 0 && f.to == 0 {
		f.from, f.to = line, line
	}
	return f
}

func specTermsFromArgs(args map[string]interface{}) []string {
	raw, ok := args["terms"].([]interface{})
	if !ok {
		if typed, typedOK := args["terms"].([]string); typedOK {
			return append([]string(nil), typed...)
		}
		return nil
	}
	terms := make([]string, 0, len(raw))
	for _, value := range raw {
		if term := strings.TrimSpace(fmt.Sprintf("%v", value)); term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func specTermsFromRaw(value interface{}) []string {
	return specTermsFromArgs(map[string]interface{}{"terms": value})
}

func specContextForSession(
	cfg config.SpecsConfig,
	sessions *browser.SessionManager,
	sessionID string,
	terms []string,
) []spec.Match {
	if !cfg.IsEnabled() || len(cfg.Sources) == 0 {
		return nil
	}
	_, docs, err := loadSpecs(cfg, map[string]interface{}{})
	if err != nil {
		return nil
	}
	route := ""
	if sessions != nil && sessionID != "" {
		if session, ok := sessions.GetSession(sessionID); ok {
			if parsed, parseErr := url.Parse(session.URL); parseErr == nil {
				route = parsed.Path
			}
		}
	}
	return spec.MatchSpecs(docs, spec.MatchInput{
		Route: route,
		Terms: terms,
		Max:   cfg.GetMaxResults(),
	}, cfg.GetMaxExcerptBytes())
}

// bindingMatches reports whether a spec satisfies the filter's binding dimension.
func bindingMatches(s spec.Spec, f specFilter) bool {
	if !f.hasBinding() {
		return true
	}
	for _, b := range s.Bindings {
		switch b.Kind {
		case "component":
			if f.component != "" && b.Target == f.component {
				return true
			}
		case "route":
			if f.route != "" && b.Target == f.route {
				return true
			}
		case "selector":
			if f.selector != "" && b.Target == f.selector {
				return true
			}
		}
	}
	return false
}

// invariantMatches reports whether an invariant satisfies the filter's file
// dimension (whole-file or overlapping line range).
func invariantMatches(inv spec.Invariant, f specFilter) bool {
	if !f.hasFile() {
		return true
	}
	if f.from > 0 && f.to > 0 {
		return inv.Covers(f.file, f.from, f.to)
	}
	return inv.InFile(f.file)
}

// selectedInvariant pairs an invariant with the spec that declared it.
type selectedInvariant struct {
	spec spec.Spec
	inv  spec.Invariant
}

// selectInvariants returns the invariants across specs that satisfy the filter.
func selectInvariants(specs []spec.Spec, f specFilter) []selectedInvariant {
	var out []selectedInvariant
	for _, s := range specs {
		if !bindingMatches(s, f) {
			continue
		}
		for _, inv := range s.Invariants {
			if invariantMatches(inv, f) {
				out = append(out, selectedInvariant{spec: s, inv: inv})
			}
		}
	}
	return out
}

func loadSpecs(cfg config.SpecsConfig, args map[string]interface{}) (string, []spec.Spec, error) {
	dir := getStringArg(args, "dir")
	if dir != "" {
		return loadSpecsDir(dir)
	}

	if cfg.IsEnabled() && len(cfg.Sources) > 0 {
		sources := make([]spec.Source, 0, len(cfg.Sources))
		names := make([]string, 0, len(cfg.Sources))
		for _, sourceCfg := range cfg.Sources {
			names = append(names, sourceCfg.Name)
			sources = append(sources, spec.Source{
				Name:    sourceCfg.Name,
				Roots:   append([]string(nil), sourceCfg.Roots...),
				Indexes: append([]string(nil), sourceCfg.Indexes...),
				Include: append([]string(nil), sourceCfg.Include...),
				Exclude: append([]string(nil), sourceCfg.Exclude...),
			})
		}
		specs, errs := spec.LoadSources(sources, spec.LoadOptions{
			MaxFiles:     cfg.GetMaxFiles(),
			MaxFileBytes: cfg.GetMaxFileBytes(),
		})
		label := strings.Join(names, ",")
		if len(errs) > 0 && len(specs) == 0 {
			return label, nil, fmt.Errorf("failed to load spec corpora: %v", errs[0])
		}
		return label, specs, nil
	}
	return loadSpecsDir(defaultSpecsDir)
}

func loadSpecsDir(dir string) (string, []spec.Spec, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return dir, nil, fmt.Errorf("specs directory not found: %s", dir)
	}
	specs, errs := spec.LoadDir(dir)
	if len(errs) > 0 && len(specs) == 0 {
		return dir, nil, fmt.Errorf("failed to load specs: %v", errs[0])
	}
	return dir, specs, nil
}

func invariantView(si selectedInvariant) map[string]interface{} {
	v := map[string]interface{}{
		"spec":   si.spec.Name,
		"name":   si.inv.Name,
		"query":  si.inv.Query,
		"expect": defaultExpect(si.inv.Expect),
	}
	if si.inv.Prose != "" {
		v["prose"] = si.inv.Prose
	}
	if si.inv.File != "" {
		v["file"] = si.inv.File
	}
	if si.inv.From > 0 {
		v["from"] = si.inv.From
		v["to"] = si.inv.To
	}
	return v
}

func defaultExpect(e string) string {
	if e == "" {
		return "present"
	}
	return e
}

// GetSpecsTool delivers spec invariants relevant to a component, route, or code
// region - spec delivery mode.
type GetSpecsTool struct {
	engine   *mangle.Engine
	specsCfg config.SpecsConfig
}

func (t *GetSpecsTool) Name() string { return "get-specs" }

func (t *GetSpecsTool) Description() string {
	return `The spec invariants that govern what you are working on, as compact facts, not whole
documents. Filter by file + from/to (or line) for "what governs these lines", or by
component, route or selector; no filter summarizes every spec. Specs are Markdown with
<!-- browsernerd:invariant ... --> tags under dir (default .browsernerd/specs).`
}

func (t *GetSpecsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dir":        map[string]interface{}{"type": "string", "description": "Default .browsernerd/specs"},
			"file":       map[string]interface{}{"type": "string", "description": "Source file"},
			"from":       map[string]interface{}{"type": "integer", "description": "From line"},
			"to":         map[string]interface{}{"type": "integer", "description": "To line"},
			"line":       map[string]interface{}{"type": "integer", "description": "One line"},
			"component":  map[string]interface{}{"type": "string", "description": "React component"},
			"route":      map[string]interface{}{"type": "string", "description": "Route"},
			"selector":   map[string]interface{}{"type": "string", "description": "CSS selector"},
			"session_id": map[string]interface{}{"type": "string", "description": "Resolve bindings in this session"},
			"terms":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Extra terms to match docs"},
		},
	}
}

func (t *GetSpecsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	dir, specs, err := loadSpecs(t.specsCfg, args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error(), "dir": dir}, nil
	}

	f := specFilterFromArgs(args)
	terms := specTermsFromArgs(args)

	// No filter: return a compact summary of available specs.
	if f.empty() && len(terms) == 0 {
		summary := make([]map[string]interface{}, 0, len(specs))
		for _, s := range specs {
			summary = append(summary, map[string]interface{}{
				"name":            s.Name,
				"path":            s.Path,
				"source":          s.Source,
				"bindings":        s.Bindings,
				"invariant_count": len(s.Invariants),
			})
		}
		return map[string]interface{}{"dir": dir, "specs": summary, "specs_loaded": len(specs)}, nil
	}

	selected := selectInvariants(specs, f)
	views := make([]map[string]interface{}, 0, len(selected))
	for _, si := range selected {
		views = append(views, invariantView(si))
	}

	out := map[string]interface{}{
		"dir":          dir,
		"specs_loaded": len(specs),
		"invariants":   views,
		"count":        len(views),
	}
	matches := spec.MatchSpecs(specs, spec.MatchInput{
		File:      f.file,
		Component: f.component,
		Route:     f.route,
		Selector:  f.selector,
		Terms:     terms,
		Max:       t.specsCfg.GetMaxResults(),
	}, t.specsCfg.GetMaxExcerptBytes())
	if len(matches) > 0 {
		out["documents"] = matches
	}

	// When a session is given, resolve each matched spec's bindings against the
	// live page so the agent learns whether the bound component/route/selector
	// is actually present and which node refs it maps to.
	if sessionID := getStringArg(args, "session_id"); sessionID != "" {
		seen := make(map[string]bool)
		resolved := make([]map[string]interface{}, 0)
		for _, si := range selected {
			if seen[si.spec.Name] {
				continue
			}
			seen[si.spec.Name] = true
			resolutions := resolveSpecBindings(ctx, t.engine, sessionID, si.spec)
			if len(resolutions) == 0 {
				continue
			}
			resolved = append(resolved, map[string]interface{}{
				"spec":     si.spec.Name,
				"bindings": resolutions,
			})
		}
		if len(resolved) > 0 {
			out["resolved_bindings"] = resolved
		}
	}

	return out, nil
}

// CheckSpecsTool evaluates spec invariants against the current fact state and
// reports violations - spec conformance mode.
type CheckSpecsTool struct {
	engine   *mangle.Engine
	specsCfg config.SpecsConfig
}

func (t *CheckSpecsTool) Name() string { return "check-specs" }

func (t *CheckSpecsTool) Description() string {
	return `Check spec invariants against the live page: each invariant's Mangle query is scored
present/absent, and a violation comes with its causal diagnosis (failed request, error
chain, slow API). Same filters as get-specs; none checks every invariant.`
}

func (t *CheckSpecsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dir":                 map[string]interface{}{"type": "string", "description": "Default .browsernerd/specs"},
			"file":                map[string]interface{}{"type": "string", "description": "Source file"},
			"from":                map[string]interface{}{"type": "integer", "description": "From line"},
			"to":                  map[string]interface{}{"type": "integer", "description": "To line"},
			"line":                map[string]interface{}{"type": "integer", "description": "One line"},
			"component":           map[string]interface{}{"type": "string", "description": "React component"},
			"route":               map[string]interface{}{"type": "string", "description": "Route"},
			"selector":            map[string]interface{}{"type": "string", "description": "CSS selector"},
			"session_id":          map[string]interface{}{"type": "string", "description": "Resolve bindings in this session"},
			"diagnose_on_failure": map[string]interface{}{"type": "boolean", "description": "Diagnose violations (default true)"},
		},
	}
}

func (t *CheckSpecsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	dir, specs, err := loadSpecs(t.specsCfg, args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error(), "dir": dir}, nil
	}

	selected := selectInvariants(specs, specFilterFromArgs(args))

	sessionID := getStringArg(args, "session_id")

	checked := 0
	violations := make([]map[string]interface{}, 0)
	violatedSpecs := make(map[string]spec.Spec)
	for _, si := range selected {
		if si.inv.Query == "" {
			continue // delivery-only prose invariant, nothing to evaluate
		}
		checked++
		name := fmt.Sprintf("%s/%s", si.spec.Name, si.inv.Name)
		res, passed := evaluateQueryExpect(ctx, t.engine, name, si.inv.Query, si.inv.Expect)
		if !passed {
			if si.inv.File != "" {
				res["file"] = si.inv.File
			}
			if si.inv.From > 0 {
				res["from"] = si.inv.From
				res["to"] = si.inv.To
			}
			violations = append(violations, res)
			violatedSpecs[si.spec.Name] = si.spec
		}
	}

	result := map[string]interface{}{
		"dir":        dir,
		"checked":    checked,
		"passed":     len(violations) == 0,
		"violations": violations,
	}

	if len(violations) > 0 && getBoolArg(args, "diagnose_on_failure", true) {
		if diag := causalDiagnosis(ctx, t.engine, sessionID, 0); len(diag) > 0 {
			result["diagnosis"] = diag
		}
	}

	// Resolve bindings for violated specs so the agent knows whether the bound
	// component/route/selector is actually on the page.
	if sessionID != "" && len(violatedSpecs) > 0 {
		resolved := make([]map[string]interface{}, 0, len(violatedSpecs))
		for name, s := range violatedSpecs {
			resolutions := resolveSpecBindings(ctx, t.engine, sessionID, s)
			if len(resolutions) == 0 {
				continue
			}
			resolved = append(resolved, map[string]interface{}{
				"spec":     name,
				"bindings": resolutions,
			})
		}
		if len(resolved) > 0 {
			result["resolved_bindings"] = resolved
		}
	}

	return result, nil
}
