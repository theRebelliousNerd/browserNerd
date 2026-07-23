package mcp

import (
	"context"
	"fmt"
	"strings"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/spec"
)

// bindingResolution reports whether a spec binding resolves to something live
// on the page, and the fiber/DOM nodes it resolves to.
type bindingResolution struct {
	Kind    string   `json:"kind"`
	Target  string   `json:"target"`
	Present bool     `json:"present"`
	Fibers  []string `json:"fibers,omitempty"`
	Nodes   []string `json:"nodes,omitempty"`
	Detail  string   `json:"detail,omitempty"`
}

// resolveBinding resolves a single binding against the current fact state for a
// session. Targets are compared in Go rather than interpolated into a Mangle
// query, so a spec-supplied target can never inject query syntax.
func resolveBinding(ctx context.Context, engine *mangle.Engine, sessionID string, b spec.Binding) bindingResolution {
	res := bindingResolution{Kind: b.Kind, Target: b.Target}
	if b.Target == "" {
		return res
	}

	switch b.Kind {
	case "component":
		res.Fibers, res.Nodes = resolveComponent(ctx, engine, sessionID, b.Target)
		res.Present = len(res.Fibers) > 0
		if res.Present && len(res.Nodes) == 0 {
			res.Detail = "component mounted but no DOM mapping captured (run reify-react/snapshot-dom)"
		}

	case "route":
		url, ok := resolveRoute(ctx, engine, sessionID, b.Target)
		res.Present = ok
		if ok {
			res.Detail = "current url: " + url
		}

	case "selector":
		res.Nodes = resolveSelector(ctx, engine, sessionID, b.Target)
		res.Present = len(res.Nodes) > 0

	default:
		res.Detail = "unknown binding kind"
	}

	return res
}

// rowsFor runs a body-less predicate query and returns the raw binding rows.
func rowsFor(ctx context.Context, engine *mangle.Engine, atom string) []mangle.QueryResult {
	rows, err := engine.Query(ctx, atom)
	if err != nil {
		return nil
	}
	return rows
}

func rowStr(row mangle.QueryResult, key string) string {
	if v, ok := row[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func sessionMatches(row mangle.QueryResult, sessionID string) bool {
	if sessionID == "" {
		return true
	}
	return rowStr(row, "S") == sessionID
}

// resolveComponent finds fiber ids for a React component name and their mapped
// DOM node ids.
func resolveComponent(ctx context.Context, engine *mangle.Engine, sessionID, name string) (fibers, nodes []string) {
	fiberSet := make(map[string]bool)
	for _, row := range rowsFor(ctx, engine, "react_component(S, F, N, P).") {
		if !sessionMatches(row, sessionID) {
			continue
		}
		if rowStr(row, "N") == name {
			f := rowStr(row, "F")
			if f != "" && !fiberSet[f] {
				fiberSet[f] = true
				fibers = append(fibers, f)
			}
		}
	}
	if len(fiberSet) == 0 {
		return nil, nil
	}

	nodeSet := make(map[string]bool)
	for _, row := range rowsFor(ctx, engine, "dom_mapping(S, F, D).") {
		if !sessionMatches(row, sessionID) {
			continue
		}
		if fiberSet[rowStr(row, "F")] {
			n := rowStr(row, "D")
			if n != "" && !nodeSet[n] {
				nodeSet[n] = true
				nodes = append(nodes, n)
			}
		}
	}
	return fibers, nodes
}

// resolveRoute reports whether the current url matches the target route. A match
// is exact, or the current url's path equals / ends with the target path.
func resolveRoute(ctx context.Context, engine *mangle.Engine, sessionID, target string) (string, bool) {
	for _, row := range rowsFor(ctx, engine, "current_url(S, U).") {
		if !sessionMatches(row, sessionID) {
			continue
		}
		url := rowStr(row, "U")
		if routeMatches(url, target) {
			return url, true
		}
	}
	return "", false
}

func routeMatches(url, target string) bool {
	if url == target {
		return true
	}
	path := urlPath(url)
	return path == target || strings.HasSuffix(path, target)
}

// urlPath extracts the path portion of a url without pulling in net/url for a
// best-effort match (avoids failing on non-standard urls).
func urlPath(url string) string {
	s := url
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "/"); j >= 0 {
			s = s[j:]
		} else {
			s = "/"
		}
	}
	for _, cut := range []string{"?", "#"} {
		if i := strings.Index(s, cut); i >= 0 {
			s = s[:i]
		}
	}
	return s
}

// resolveSelector matches a target against DOM node ids / data-testid. Supported
// forms: "#id", "[data-testid=x]" or bare "x" (matched against id or
// data-testid).
func resolveSelector(ctx context.Context, engine *mangle.Engine, sessionID, target string) []string {
	key, value := parseSelector(target)

	nodeSet := make(map[string]bool)
	var nodes []string
	for _, row := range rowsFor(ctx, engine, "dom_attr(S, Node, K, V).") {
		if !sessionMatches(row, sessionID) {
			continue
		}
		attrKey := rowStr(row, "K")
		attrVal := rowStr(row, "V")
		if attrVal != value {
			continue
		}
		if (key == "" && (attrKey == "id" || attrKey == "data-testid")) || attrKey == key {
			n := rowStr(row, "Node")
			if n != "" && !nodeSet[n] {
				nodeSet[n] = true
				nodes = append(nodes, n)
			}
		}
	}
	return nodes
}

// parseSelector returns (attrKey, value) for a simple selector. An empty key
// means "id or data-testid".
func parseSelector(target string) (string, string) {
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "#") {
		return "id", strings.TrimPrefix(t, "#")
	}
	if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		inner := t[1 : len(t)-1]
		if idx := strings.Index(inner, "="); idx >= 0 {
			key := strings.TrimSpace(inner[:idx])
			val := strings.Trim(strings.TrimSpace(inner[idx+1:]), `"'`)
			return key, val
		}
		return inner, ""
	}
	return "", t
}

// resolveSpecBindings resolves every binding on a spec.
func resolveSpecBindings(ctx context.Context, engine *mangle.Engine, sessionID string, s spec.Spec) []bindingResolution {
	out := make([]bindingResolution, 0, len(s.Bindings))
	for _, b := range s.Bindings {
		out = append(out, resolveBinding(ctx, engine, sessionID, b))
	}
	return out
}
