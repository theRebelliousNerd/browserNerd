package mangle

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"codeberg.org/TauCeti/mangle-go/ast"
	"codeberg.org/TauCeti/mangle-go/parse"
)

// conjunctionHead is the head a conjunctive query is parsed under before its
// variables are known (the parser rejects names that start with "_").
const conjunctionHead = "browsernerd_conjunction"

// parseQueryClause parses a query into one clause. A bare conjunction
// (`a(X, Y), b(Y, Z).`) becomes a clause whose head carries every variable
// the conjunction binds, in order of first appearance, so the query returns
// all of them.
func parseQueryClause(queryStr string) (ast.Clause, error) {
	if !strings.Contains(queryStr, ":-") && hasTopLevelComma(queryStr) {
		body := strings.TrimSuffix(strings.TrimSpace(queryStr), ".")
		unit, err := parse.Unit(bytes.NewReader([]byte(conjunctionHead + "() :- " + body + ".")))
		if err != nil {
			return ast.Clause{}, fmt.Errorf("parse query: %w", err)
		}
		if len(unit.Clauses) != 1 {
			return ast.Clause{}, fmt.Errorf("query must contain exactly one clause")
		}
		clause := unit.Clauses[0]
		vars := boundVariables(clause.Premises)
		if len(vars) == 0 {
			return ast.Clause{}, fmt.Errorf("conjunctive query binds no named variable; name the values to return (e.g. U instead of _)")
		}
		args := make([]ast.BaseTerm, len(vars))
		for i, v := range vars {
			args[i] = v
		}
		clause.Head = ast.Atom{Predicate: ast.PredicateSym{Symbol: queryResultPredicate, Arity: len(args)}, Args: args}
		return clause, nil
	}

	unit, err := parse.Unit(bytes.NewReader([]byte(queryStr)))
	if err != nil {
		return ast.Clause{}, fmt.Errorf("parse query: %w", err)
	}
	if len(unit.Clauses) == 0 {
		return ast.Clause{}, fmt.Errorf("no query found")
	}
	if len(unit.Clauses) != 1 {
		return ast.Clause{}, fmt.Errorf("query must contain exactly one clause")
	}
	return unit.Clauses[0], nil
}

// hasTopLevelComma reports a comma outside any parentheses, brackets and
// string literals: the mark of a conjunction.
func hasTopLevelComma(s string) bool {
	depth := 0
	var quote rune
	escaped := false
	for _, r := range s {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == quote:
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			quote = r
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// boundVariables lists the named variables a conjunction binds, in order of
// first appearance. Variables that appear only under negation are never
// bound, so they are not returned.
func boundVariables(premises []ast.Term) []ast.Variable {
	var out []ast.Variable
	seen := make(map[string]bool)
	add := func(term ast.BaseTerm) {
		v, ok := term.(ast.Variable)
		if !ok || v.Symbol == "_" || seen[v.Symbol] {
			return
		}
		seen[v.Symbol] = true
		out = append(out, v)
	}
	var walk func(term ast.Term)
	walk = func(term ast.Term) {
		switch t := term.(type) {
		case ast.Atom:
			for _, arg := range t.Args {
				add(arg)
			}
		case ast.Eq:
			add(t.Left)
			add(t.Right)
		case ast.TemporalLiteral:
			walk(t.Literal)
		case ast.TemporalAtom:
			walk(t.Atom)
		}
	}
	for _, premise := range premises {
		walk(premise)
	}
	return out
}

// queryAtoms returns the predicate atoms a clause reads: its premises, or its
// head when it has none.
func queryAtoms(clause ast.Clause) []ast.Atom {
	if len(clause.Premises) == 0 {
		return []ast.Atom{clause.Head}
	}
	var atoms []ast.Atom
	var walk func(term ast.Term)
	walk = func(term ast.Term) {
		switch t := term.(type) {
		case ast.Atom:
			atoms = append(atoms, t)
		case ast.NegAtom:
			atoms = append(atoms, t.Atom)
		case ast.TemporalLiteral:
			walk(t.Literal)
		case ast.TemporalAtom:
			walk(t.Atom)
		}
	}
	for _, premise := range clause.Premises {
		walk(premise)
	}
	return atoms
}

// knownPredicatesLocked maps every predicate name the program declares or
// derives, or the buffer holds facts of, to its arities.
func (e *Engine) knownPredicatesLocked() map[string][]int {
	known := make(map[string][]int)
	addArity := func(name string, arity int) {
		for _, existing := range known[name] {
			if existing == arity {
				return
			}
		}
		known[name] = append(known[name], arity)
	}
	if e.programInfo != nil {
		for sym := range e.programInfo.Decls {
			addArity(sym.Symbol, sym.Arity)
		}
	}
	for name, indices := range e.index {
		for _, idx := range indices {
			if idx >= 0 && idx < len(e.facts) {
				addArity(name, len(e.facts[idx].Args))
			}
		}
	}
	return known
}

// signatureLocked renders a predicate's declared signature with its argument
// names, e.g. net_response(SessionId, Id, Status, Latency, Duration).
func (e *Engine) signatureLocked(name string, arity int) string {
	args := make([]string, arity)
	for i := range args {
		args[i] = fmt.Sprintf("Arg%d", i+1)
	}
	if e.programInfo != nil {
		for sym, decl := range e.programInfo.Decls {
			if sym.Symbol == name && sym.Arity == arity && decl != nil && !decl.IsSynthetic() && len(decl.DeclaredAtom.Args) == arity {
				for i, arg := range decl.DeclaredAtom.Args {
					args[i] = arg.String()
				}
				break
			}
		}
	}
	return name + "(" + strings.Join(args, ", ") + ")"
}

func (e *Engine) moduleOfLocked(name string) string {
	if module, ok := e.declModule[name]; ok {
		return module
	}
	if e.isDerivedLocked(name) {
		return "derived"
	}
	return "pushed facts"
}

// checkAtomsLocked rejects an atom that names an unknown predicate or gives a
// known one the wrong number of arguments. Such a query can never match, and
// answering it with zero rows reads as "nothing happened".
func (e *Engine) checkAtomsLocked(atoms []ast.Atom, extra map[string][]int) error {
	known := e.knownPredicatesLocked()
	for name, arities := range extra {
		known[name] = append(known[name], arities...)
	}
	for _, atom := range atoms {
		if atom.Predicate.IsBuiltin() || strings.HasPrefix(atom.Predicate.Symbol, ":") {
			continue
		}
		name := atom.Predicate.Symbol
		arity := len(atom.Args)
		arities, ok := known[name]
		if !ok {
			return e.unknownPredicateError(name, known)
		}
		matched := false
		for _, a := range arities {
			if a == arity {
				matched = true
				break
			}
		}
		if !matched {
			signatures := make([]string, 0, len(arities))
			for _, a := range arities {
				signatures = append(signatures, e.signatureLocked(name, a))
			}
			return fmt.Errorf("%s takes %s, not %d: %s [%s]", name, plural(arities[0], "argument"), arity, strings.Join(signatures, " | "), e.moduleOfLocked(name))
		}
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (e *Engine) unknownPredicateError(name string, known map[string][]int) error {
	suggestions := closestNames(name, known, 3)
	if len(suggestions) == 0 {
		return fmt.Errorf("unknown predicate %s: no Decl declares it and no fact of it is buffered (list predicates with operation schema)", name)
	}
	rendered := make([]string, 0, len(suggestions))
	for _, candidate := range suggestions {
		rendered = append(rendered, e.signatureLocked(candidate, known[candidate][0]))
	}
	return fmt.Errorf("unknown predicate %s; did you mean %s?", name, strings.Join(rendered, " or "))
}

// closestNames ranks known names by edit distance to name, keeping close ones
// and names that contain it or are contained by it.
func closestNames(name string, known map[string][]int, limit int) []string {
	type scored struct {
		name  string
		score int
	}
	threshold := len(name) / 3
	if threshold < 2 {
		threshold = 2
	}
	var candidates []scored
	for candidate := range known {
		if strings.HasPrefix(candidate, "__") {
			continue
		}
		distance := levenshtein(name, candidate)
		contains := strings.Contains(candidate, name) || strings.Contains(name, candidate)
		if distance > threshold && !contains {
			continue
		}
		score := distance
		if contains {
			score = distance / 2
		}
		candidates = append(candidates, scored{candidate, score})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		return candidates[i].name < candidates[j].name
	})
	out := make([]string, 0, limit)
	for i := 0; i < len(candidates) && i < limit; i++ {
		out = append(out, candidates[i].name)
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = minOf(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func minOf(values ...int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

// checkQueryClauseLocked validates every predicate a query reads.
func (e *Engine) checkQueryClauseLocked(clause ast.Clause) error {
	return e.checkAtomsLocked(queryAtoms(clause), nil)
}

// checkPredicateNameLocked validates a bare predicate name.
func (e *Engine) checkPredicateNameLocked(name string) error {
	known := e.knownPredicatesLocked()
	if _, ok := known[name]; ok {
		return nil
	}
	return e.unknownPredicateError(name, known)
}

// checkRuleUnitLocked validates the premises of submitted rules. Predicates
// the submission itself declares or defines are known to it.
func (e *Engine) checkRuleUnitLocked(unit parse.SourceUnit) error {
	extra := make(map[string][]int)
	for _, decl := range unit.Decls {
		sym := decl.DeclaredAtom.Predicate
		extra[sym.Symbol] = append(extra[sym.Symbol], sym.Arity)
	}
	for _, clause := range unit.Clauses {
		sym := clause.Head.Predicate
		extra[sym.Symbol] = append(extra[sym.Symbol], sym.Arity)
	}
	for _, clause := range unit.Clauses {
		if len(clause.Premises) == 0 {
			continue
		}
		if err := e.checkAtomsLocked(queryAtoms(clause), extra); err != nil {
			return fmt.Errorf("rule %s: %w", clause.Head.Predicate.Symbol, err)
		}
	}
	return nil
}
