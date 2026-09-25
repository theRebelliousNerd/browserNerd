package mangle

import (
	"fmt"
	"sort"
	"strings"
)

// PredicateInfo describes one predicate an agent can query.
type PredicateInfo struct {
	Name string `json:"name"`
	// Signature carries the declared argument names, e.g.
	// net_response(SessionId, Id, Status, Latency, Duration).
	Signature string `json:"signature"`
	Arity     int    `json:"arity"`
	// Module is builtin:<module>, project:<module>, runtime (submitted),
	// derived (a rule head without a Decl), or pushed facts.
	Module string `json:"module"`
	// Kind is base (facts are captured or pushed) or derived (rules produce it).
	Kind string `json:"kind"`
	// Facts counts buffered facts of a base predicate.
	Facts int `json:"facts"`
}

// IsDerived reports whether a rule derives predicate; its facts then never
// enter the buffer and are read through Query or Evaluate.
func (e *Engine) IsDerived(predicate string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isDerivedLocked(predicate)
}

// Schema lists the predicates whose name contains filter (every predicate for
// an empty filter), ordered by module then name. Internal mt_ mirrors and
// engine-private predicates are left out.
func (e *Engine) Schema(filter string) []PredicateInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	filter = strings.ToLower(strings.TrimSpace(filter))
	seen := make(map[string]bool)
	out := make([]PredicateInfo, 0)
	add := func(name string, arity int) {
		key := fmt.Sprintf("%s/%d", name, arity)
		if seen[key] || strings.HasPrefix(name, "__") || strings.HasPrefix(name, "mt_") {
			return
		}
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			return
		}
		seen[key] = true
		kind := "base"
		if e.isDerivedLocked(name) {
			kind = "derived"
		}
		out = append(out, PredicateInfo{
			Name:      name,
			Signature: e.signatureLocked(name, arity),
			Arity:     arity,
			Module:    e.moduleOfLocked(name),
			Kind:      kind,
			Facts:     len(e.index[name]),
		})
	}
	if e.programInfo != nil {
		for sym := range e.programInfo.Decls {
			add(sym.Symbol, sym.Arity)
		}
	}
	for name, indices := range e.index {
		if len(indices) > 0 {
			add(name, len(e.facts[indices[0]].Args))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].Name < out[j].Name
	})
	return out
}
