package mcp

import (
	"context"
	"fmt"
	"os"

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

func loadSpecsDir(args map[string]interface{}) (string, []spec.Spec, error) {
	dir := getStringArg(args, "dir")
	if dir == "" {
		dir = defaultSpecsDir
	}
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
// region — spec delivery mode.
type GetSpecsTool struct {
	engine *mangle.Engine
}

func (t *GetSpecsTool) Name() string { return "get-specs" }

func (t *GetSpecsTool) Description() string {
	return `Deliver frontend spec invariants relevant to what you're working on.

TOKEN COST: Low. Returns only the invariants that govern your target, as
compact facts — not whole documents.

FILTER BY (any combination):
- file + from/to (or line): invariants whose code line-range overlaps yours.
  This is "I'm editing these lines, what governs them?"
- component: invariants on a spec bound to that React component.
- route: invariants on a spec bound to that route/path.
- selector: invariants on a spec bound to that selector.

With no filter, returns a summary of all specs.

Specs are Markdown docs with YAML frontmatter + inline
<!-- browsernerd:invariant ... --> tags, loaded from "dir" (default
.browsernerd/specs).

Returns: {dir, specs_loaded, invariants[]} or {dir, specs[]} for the summary.`
}

func (t *GetSpecsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dir":       map[string]interface{}{"type": "string", "description": "Specs directory (default .browsernerd/specs)."},
			"file":      map[string]interface{}{"type": "string", "description": "Source file you're working in."},
			"from":      map[string]interface{}{"type": "integer", "description": "Start line of the region you're working on."},
			"to":        map[string]interface{}{"type": "integer", "description": "End line of the region you're working on."},
			"line":      map[string]interface{}{"type": "integer", "description": "A single line, shorthand for from==to."},
			"component":  map[string]interface{}{"type": "string", "description": "React component name to match a spec binding."},
			"route":      map[string]interface{}{"type": "string", "description": "Route/path to match a spec binding."},
			"selector":   map[string]interface{}{"type": "string", "description": "Selector to match a spec binding."},
			"session_id": map[string]interface{}{"type": "string", "description": "Resolve bindings against this live session (component->fiber->DOM, route, selector)."},
		},
	}
}

func (t *GetSpecsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	dir, specs, err := loadSpecsDir(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error(), "dir": dir}, nil
	}

	f := specFilterFromArgs(args)

	// No filter: return a compact summary of available specs.
	if f.empty() {
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
// reports violations — spec conformance mode.
type CheckSpecsTool struct {
	engine *mangle.Engine
}

func (t *CheckSpecsTool) Name() string { return "check-specs" }

func (t *CheckSpecsTool) Description() string {
	return `Check frontend spec invariants against the current browser state.

Evaluates each invariant's Mangle query (scored present/absent, like run-test)
and reports violations. On any violation, attaches a compact causal diagnosis
(error_chain, failed_request, slow_api, ...) so a broken spec returns a root
cause, not a trace dump.

Scope with the same filters as get-specs (file/from/to, component, route,
selector), or omit to check every loaded invariant.

Returns: {dir, checked, passed, violations[], diagnosis?}.`
}

func (t *CheckSpecsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dir":                 map[string]interface{}{"type": "string", "description": "Specs directory (default .browsernerd/specs)."},
			"file":                map[string]interface{}{"type": "string", "description": "Scope to invariants governing this file."},
			"from":                map[string]interface{}{"type": "integer", "description": "Start line of the region to scope to."},
			"to":                  map[string]interface{}{"type": "integer", "description": "End line of the region to scope to."},
			"line":                map[string]interface{}{"type": "integer", "description": "Single line, shorthand for from==to."},
			"component":           map[string]interface{}{"type": "string", "description": "Scope to specs bound to this component."},
			"route":               map[string]interface{}{"type": "string", "description": "Scope to specs bound to this route."},
			"selector":            map[string]interface{}{"type": "string", "description": "Scope to specs bound to this selector."},
			"session_id":          map[string]interface{}{"type": "string", "description": "Resolve each violated spec's bindings against this live session."},
			"diagnose_on_failure": map[string]interface{}{"type": "boolean", "description": "Attach causal facts on violation (default true)."},
		},
	}
}

func (t *CheckSpecsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	dir, specs, err := loadSpecsDir(args)
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
		if diag := causalDiagnosis(ctx, t.engine); len(diag) > 0 {
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
