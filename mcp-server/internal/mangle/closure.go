package mangle

import (
	"sort"
	"strings"

	"codeberg.org/TauCeti/mangle-go/analysis"
	"codeberg.org/TauCeti/mangle-go/ast"
)

// A read evaluates only what it reads: the rules of the queried predicate and,
// transitively, of every predicate those rules use. Querying failed_request
// then costs the three rules it depends on, not the ~150 rules of the whole
// program, and a costly rule elsewhere (a correlation, a runtime rule) can
// neither slow nor fail a read that does not use it.

// premisePredicates lists the predicate symbols a clause body reads.
func premisePredicates(clause ast.Clause) []string {
	var out []string
	var walk func(term ast.Term)
	walk = func(term ast.Term) {
		switch t := term.(type) {
		case ast.Atom:
			out = append(out, t.Predicate.Symbol)
		case ast.NegAtom:
			out = append(out, t.Atom.Predicate.Symbol)
		case ast.TemporalLiteral:
			walk(t.Literal)
		case ast.TemporalAtom:
			walk(t.Atom)
		}
	}
	for _, premise := range clause.Premises {
		walk(premise)
	}
	return out
}

// closureProgram restricts pi to the rules the targets depend on. A nil or
// empty target list means the whole program.
func closureProgram(pi *analysis.ProgramInfo, targets []string) *analysis.ProgramInfo {
	if len(targets) == 0 {
		return pi
	}
	rulesByHead := make(map[string][]ast.Clause)
	for _, rule := range pi.Rules {
		head := rule.Head.Predicate.Symbol
		rulesByHead[head] = append(rulesByHead[head], rule)
	}
	need := make(map[string]bool)
	queue := append([]string(nil), targets...)
	for len(queue) > 0 {
		sym := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if need[sym] {
			continue
		}
		need[sym] = true
		for _, rule := range rulesByHead[sym] {
			for _, dep := range premisePredicates(rule) {
				if !need[dep] {
					queue = append(queue, dep)
				}
			}
		}
	}

	sub := &analysis.ProgramInfo{
		EdbPredicates: pi.EdbPredicates,
		IdbPredicates: make(map[ast.PredicateSym]struct{}),
		Decls:         pi.Decls,
	}
	for sym := range pi.IdbPredicates {
		if need[sym.Symbol] {
			sub.IdbPredicates[sym] = struct{}{}
		}
	}
	for _, rule := range pi.Rules {
		if need[rule.Head.Predicate.Symbol] {
			sub.Rules = append(sub.Rules, rule)
		}
	}
	for i, fact := range pi.InitialFacts {
		if !need[fact.Predicate.Symbol] {
			continue
		}
		sub.InitialFacts = append(sub.InitialFacts, fact)
		var interval *ast.Interval
		if i < len(pi.InitialFactTimes) {
			interval = pi.InitialFactTimes[i]
		}
		sub.InitialFactTimes = append(sub.InitialFactTimes, interval)
	}
	return sub
}

// closureKey names a target set for the derivation cache.
func closureKey(targets []string) string {
	if len(targets) == 0 {
		return "*"
	}
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}
