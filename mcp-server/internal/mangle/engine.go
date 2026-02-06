package mangle

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/config"

	"github.com/google/mangle/analysis"
	"github.com/google/mangle/ast"
	"github.com/google/mangle/engine"
	"github.com/google/mangle/factstore"
	"github.com/google/mangle/parse"
)

// Fact represents a normalized event emitted by the browser bridge.
type Fact struct {
	Predicate string        `json:"predicate"`
	Args      []interface{} `json:"args"`
	Timestamp time.Time     `json:"timestamp"`
}

// QueryResult represents a binding of variables to values from a Mangle query.
type QueryResult map[string]interface{}

// defaultLowValuePredicates returns predicates that can be sampled under load.
// High-value predicates (errors, failures, navigation) are never sampled.
func defaultLowValuePredicates() map[string]bool {
	return map[string]bool{
		"dom_node":    true, // DOM snapshots are verbose
		"dom_attr":    true, // DOM attributes are high-volume
		"dom_text":    true, // DOM text is high-volume
		"react_prop":  true, // React props are verbose
		"react_state": true, // React state is verbose
		"net_header":  true, // Headers are verbose
		"input_event": true, // Input events can be high-frequency
	}
	// NOT sampled (high-value):
	// - console_event (errors are critical)
	// - net_request, net_response (network is diagnostic)
	// - navigation_event, current_url (state changes)
	// - click_event (user actions)
	// - state_change (app state)
}

// Engine wraps the Mangle deductive database with browser-specific fact management.
// This is the PRODUCTION-READY version that properly integrates Mangle's engine.EvalProgram.
type Engine struct {
	cfg          config.MangleConfig
	mu           sync.RWMutex
	schemaLoaded bool

	// Mangle core components
	programInfo *analysis.ProgramInfo
	store       factstore.FactStore

	// Fact buffer for temporal queries
	facts []Fact

	// Predicate index for O(m) lookup instead of O(n)
	index map[string][]int

	// Adaptive sampling state (PRD Section 3.5)
	samplingRate     float64            // Current sampling rate (1.0 = accept all)
	predicateCounts  map[string]int     // Count of facts per predicate in current window
	lowValuePredicates map[string]bool  // Predicates considered low-value for sampling

	// Watch mode subscriptions (PRD Section 5.2)
	subscriptions map[string][]chan WatchEvent // predicate -> list of subscriber channels
	subMu         sync.RWMutex                  // Separate mutex for subscription management
}

// WatchEvent is emitted when a watched predicate derives new facts.
type WatchEvent struct {
	Predicate string    `json:"predicate"`
	Facts     []Fact    `json:"facts"`
	Timestamp time.Time `json:"timestamp"`
}

func NewEngine(cfg config.MangleConfig) (*Engine, error) {
	e := &Engine{
		cfg:                cfg,
		facts:              make([]Fact, 0, cfg.FactBufferLimit),
		index:              make(map[string][]int),
		store:              factstore.NewSimpleInMemoryStore(),
		samplingRate:       1.0,
		predicateCounts:    make(map[string]int),
		lowValuePredicates: defaultLowValuePredicates(),
		subscriptions:      make(map[string][]chan WatchEvent),
	}

	if cfg.Enable && cfg.SchemaPath != "" {
		if err := e.LoadSchema(cfg.SchemaPath); err != nil {
			return nil, err
		}
	}

	return e, nil
}

// LoadSchema parses a Mangle schema file, analyzes it, and prepares the engine for evaluation.
// This REPLACES the stub implementation that discarded the parsed AST.
func (e *Engine) LoadSchema(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	// Parse the Mangle source
	sourceUnit, err := parse.Unit(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	// Analyze the program (stratification, safety checks)
	// extraPredicates is empty map for base schema
	programInfo, err := analysis.AnalyzeOneUnit(sourceUnit, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return fmt.Errorf("analyze schema: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.programInfo = programInfo
	e.schemaLoaded = true

	return nil
}

// AddRule dynamically adds a Mangle rule to the program for runtime assertions.
// This enables the PRD's "submit logic rules for continuous evaluation" vision.
func (e *Engine) AddRule(ruleSource string) error {
	if !e.cfg.Enable {
		return nil
	}

	// Parse the rule
	sourceUnit, err := parse.Unit(bytes.NewReader([]byte(ruleSource)))
	if err != nil {
		return fmt.Errorf("parse rule: %w", err)
	}

	// Analyze with existing program's declarations as context
	e.mu.Lock()
	defer e.mu.Unlock()

	existingDecls := make(map[ast.PredicateSym]ast.Decl)
	if e.programInfo != nil && e.programInfo.Decls != nil {
		// Convert pointer map to value map
		for k, v := range e.programInfo.Decls {
			if v != nil {
				existingDecls[k] = *v
			}
		}
	}

	newProgramInfo, err := analysis.AnalyzeOneUnit(sourceUnit, existingDecls)
	if err != nil {
		return fmt.Errorf("analyze rule: %w", err)
	}

	// Merge declarations from new program into existing
	if e.programInfo == nil {
		e.programInfo = newProgramInfo
	} else {
		for k, v := range newProgramInfo.Decls {
			e.programInfo.Decls[k] = v
		}
	}

	return nil
}

// AddFacts appends incoming facts to both the temporal buffer and the Mangle store.
// Implements adaptive sampling (PRD Section 3.5) to drop low-value events under load.
func (e *Engine) AddFacts(ctx context.Context, facts []Fact) error {
	if !e.cfg.Enable {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Adaptive sampling: adjust rate based on buffer pressure
	e.updateSamplingRate()

	// Filter facts through adaptive sampling
	filtered := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if e.shouldAcceptFact(f) {
			filtered = append(filtered, f)
			e.predicateCounts[f.Predicate]++
		}
	}

	// Add to temporal buffer with circular buffering
	baseIdx := len(e.facts)
	e.facts = append(e.facts, filtered...)
	if e.cfg.FactBufferLimit > 0 && len(e.facts) > e.cfg.FactBufferLimit {
		trimCount := len(e.facts) - e.cfg.FactBufferLimit
		e.facts = e.facts[trimCount:]
		baseIdx -= trimCount

		// Rebuild index after trim
		e.rebuildIndex()
	} else {
		// Incremental index update
		for i, f := range filtered {
			idx := baseIdx + i
			if idx >= 0 && idx < len(e.facts) {
				e.index[f.Predicate] = append(e.index[f.Predicate], idx)
			}
		}
	}

	// Add to Mangle store for rule evaluation
	for _, f := range filtered {
		atom, err := e.factToAtom(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[DEBUG] factToAtom failed for %s: %v\n", f.Predicate, err)
			continue // Skip malformed facts
		}
		added := e.store.Add(atom)
		fmt.Fprintf(os.Stderr, "[DEBUG] Added fact to store: %s (Arity: %d) -> %v\n", f.Predicate, len(f.Args), added)
	}

	// Trigger incremental evaluation if schema loaded
	if e.schemaLoaded && e.programInfo != nil {
		// Incremental evaluation (semi-naive)
		if err := engine.EvalProgram(e.programInfo, e.store); err != nil {
			fmt.Fprintf(os.Stderr, "[DEBUG] EvalProgram failed: %v\n", err)
			return fmt.Errorf("eval program after fact insertion: %w", err)
		}

		// Check watched predicates and notify subscribers (Watch Mode - PRD 5.2)
		e.checkAndNotifyWatchers()
	}

	return nil
}

// checkAndNotifyWatchers evaluates watched predicates and notifies subscribers.
func (e *Engine) checkAndNotifyWatchers() {
	watchedPredicates := e.WatchPredicates()
	if len(watchedPredicates) == 0 {
		return
	}

	for _, predicate := range watchedPredicates {
		// Query the store for derived facts
		predSym := ast.PredicateSym{Symbol: predicate, Arity: -1}
		wildcardAtom := ast.Atom{Predicate: predSym}

		var derivedFacts []Fact
		_ = e.store.GetFacts(wildcardAtom, func(atom ast.Atom) error {
			fact, err := e.atomToFact(atom)
			if err == nil {
				derivedFacts = append(derivedFacts, fact)
			}
			return nil
		})

		if len(derivedFacts) > 0 {
			e.notifySubscribers(predicate, derivedFacts)
		}
	}
}

// updateSamplingRate adjusts sampling based on buffer pressure (PRD Section 3.5).
// When buffer is >80% full, start dropping low-value facts.
func (e *Engine) updateSamplingRate() {
	if e.cfg.FactBufferLimit <= 0 {
		e.samplingRate = 1.0
		return
	}

	fillRatio := float64(len(e.facts)) / float64(e.cfg.FactBufferLimit)

	switch {
	case fillRatio < 0.5:
		e.samplingRate = 1.0 // Accept all
	case fillRatio < 0.7:
		e.samplingRate = 0.8 // Drop 20% of low-value
	case fillRatio < 0.85:
		e.samplingRate = 0.5 // Drop 50% of low-value
	case fillRatio < 0.95:
		e.samplingRate = 0.2 // Drop 80% of low-value
	default:
		e.samplingRate = 0.1 // Drop 90% of low-value (emergency)
	}
}

// shouldAcceptFact determines if a fact should be accepted based on adaptive sampling.
// High-value facts (errors, navigation, network) are always accepted.
func (e *Engine) shouldAcceptFact(f Fact) bool {
	// High-value predicates always accepted
	if !e.lowValuePredicates[f.Predicate] {
		return true
	}

	// Full sampling rate means accept all
	if e.samplingRate >= 1.0 {
		return true
	}

	// Probabilistic sampling for low-value predicates
	return rand.Float64() < e.samplingRate
}

// SamplingRate returns the current adaptive sampling rate (for diagnostics).
func (e *Engine) SamplingRate() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.samplingRate
}

// Subscribe registers a channel to receive events when a predicate derives new facts.
// Returns a subscription ID for later unsubscription.
// This implements PRD Section 5.2: Watch Mode for continuous rule evaluation.
func (e *Engine) Subscribe(predicate string, ch chan WatchEvent) string {
	e.subMu.Lock()
	defer e.subMu.Unlock()

	e.subscriptions[predicate] = append(e.subscriptions[predicate], ch)
	// Return a unique ID (channel address as string for simplicity)
	return fmt.Sprintf("%s:%p", predicate, ch)
}

// Unsubscribe removes a channel from the subscription list for a predicate.
func (e *Engine) Unsubscribe(predicate string, ch chan WatchEvent) {
	e.subMu.Lock()
	defer e.subMu.Unlock()

	channels := e.subscriptions[predicate]
	for i, c := range channels {
		if c == ch {
			e.subscriptions[predicate] = append(channels[:i], channels[i+1:]...)
			break
		}
	}
}

// notifySubscribers sends events to all subscribers watching a predicate.
// Called after rule evaluation when new facts are derived.
func (e *Engine) notifySubscribers(predicate string, facts []Fact) {
	e.subMu.RLock()
	channels := e.subscriptions[predicate]
	e.subMu.RUnlock()

	if len(channels) == 0 || len(facts) == 0 {
		return
	}

	event := WatchEvent{
		Predicate: predicate,
		Facts:     facts,
		Timestamp: time.Now(),
	}

	for _, ch := range channels {
		select {
		case ch <- event:
			// Sent successfully
		default:
			// Channel full, skip (non-blocking)
		}
	}
}

// WatchPredicates returns a list of predicates that have active subscriptions.
func (e *Engine) WatchPredicates() []string {
	e.subMu.RLock()
	defer e.subMu.RUnlock()

	predicates := make([]string, 0, len(e.subscriptions))
	for p, chs := range e.subscriptions {
		if len(chs) > 0 {
			predicates = append(predicates, p)
		}
	}
	return predicates
}

// Query executes a Mangle query with variable binding and returns all satisfying assignments.
// This is the REAL Datalog query interface that was missing from the stub.
// Falls back to direct buffer search if Mangle store query returns no results.
func (e *Engine) Query(ctx context.Context, queryStr string) ([]QueryResult, error) {
	if !e.cfg.Enable || !e.schemaLoaded {
		return nil, fmt.Errorf("engine not ready")
	}

	// Parse the query
	sourceUnit, err := parse.Unit(bytes.NewReader([]byte(queryStr)))
	if err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}

	// Extract the query atom (should be a single query statement)
	if len(sourceUnit.Clauses) == 0 {
		return nil, fmt.Errorf("no query found")
	}

	clause := sourceUnit.Clauses[0]
	// In Mangle v0.4.0, queries are just Clauses with a Head atom
	queryAtom := clause.Head

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Get all facts matching the query predicate using callback pattern
	results := make([]QueryResult, 0)

	err = e.store.GetFacts(queryAtom, func(atom ast.Atom) error {
		result := make(QueryResult)

		// Bind variables from the query to fact arguments
		for i, arg := range queryAtom.Args {
			if i >= len(atom.Args) {
				break
			}

			// If query arg is a variable, bind it
			if varArg, ok := arg.(ast.Variable); ok {
				result[varArg.Symbol] = e.convertConstant(atom.Args[i])
			}
		}

		results = append(results, result)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("query execution: %w", err)
	}

	// Fallback: If Mangle store returned nothing, search the temporal buffer directly
	// This handles cases where facts exist but store lookup fails due to arity mismatch
	if len(results) == 0 {
		predName := queryAtom.Predicate.Symbol
		bufferResults := e.queryBufferDirect(predName, queryAtom.Args)
		results = append(results, bufferResults...)
	}

	return results, nil
}

// queryBufferDirect searches the temporal buffer for facts matching predicate and args pattern.
// This is a fallback for when the Mangle store GetFacts doesn't match due to arity issues.
func (e *Engine) queryBufferDirect(predicate string, queryArgs []ast.BaseTerm) []QueryResult {
	results := make([]QueryResult, 0)

	indices, exists := e.index[predicate]
	if !exists {
		return results
	}

	for _, idx := range indices {
		if idx < 0 || idx >= len(e.facts) {
			continue
		}
		f := e.facts[idx]

		// Check if fact matches the query pattern
		if len(queryArgs) > 0 && len(f.Args) < len(queryArgs) {
			continue
		}

		result := make(QueryResult)
		matches := true

		for i, qArg := range queryArgs {
			if i >= len(f.Args) {
				break
			}

			// Check if query arg is a variable (starts with uppercase in Mangle convention)
			if varArg, ok := qArg.(ast.Variable); ok {
				// Bind variable to fact value
				result[varArg.Symbol] = f.Args[i]
			} else if constArg, ok := qArg.(ast.Constant); ok {
				// Constant - must match exactly
				factVal := fmt.Sprintf("%v", f.Args[i])
				queryVal := e.convertConstant(constArg)
				if factVal != fmt.Sprintf("%v", queryVal) {
					matches = false
					break
				}
			}
		}

		if matches {
			results = append(results, result)
		}
	}

	return results
}

// Evaluate runs full program evaluation and returns derived facts for a specific predicate.
func (e *Engine) Evaluate(ctx context.Context, predicate string) ([]Fact, error) {
	if !e.cfg.Enable || !e.schemaLoaded {
		return nil, fmt.Errorf("engine not ready")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Run evaluation
	if err := engine.EvalProgram(e.programInfo, e.store); err != nil {
		return nil, fmt.Errorf("eval program: %w", err)
	}

	// Find the correct arity from declarations
	arity := -1
	for sym := range e.programInfo.Decls {
		if sym.Symbol == predicate {
			arity = sym.Arity
			break
		}
	}

	// Get derived facts using callback pattern
	predSym := ast.PredicateSym{Symbol: predicate, Arity: arity}
	facts := make([]Fact, 0)

	// Create a query atom for the predicate
	// If arity is known, use it with wildcards for args
	var queryAtom ast.Atom
	if arity >= 0 {
		args := make([]ast.BaseTerm, arity)
		for i := 0; i < arity; i++ {
			// Using a variable as a wildcard
			args[i] = ast.Variable{Symbol: fmt.Sprintf("V%d", i)}
		}
		queryAtom = ast.Atom{Predicate: predSym, Args: args}
	} else {
		// Fallback to -1 if not found in Decls
		queryAtom = ast.Atom{Predicate: predSym}
	}

	err := e.store.GetFacts(queryAtom, func(atom ast.Atom) error {
		fact, err := e.atomToFact(atom)
		if err != nil {
			return nil // Skip malformed atoms
		}
		facts = append(facts, fact)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("get facts: %w", err)
	}

	return facts, nil
}

// QueryTemporal queries facts within a time window (temporal reasoning).
func (e *Engine) QueryTemporal(predicate string, after, before time.Time) []Fact {
	e.mu.RLock()
	defer e.mu.RUnlock()

	results := make([]Fact, 0)
	indices, exists := e.index[predicate]
	if !exists {
		return results
	}

	for _, idx := range indices {
		if idx < 0 || idx >= len(e.facts) {
			continue
		}
		f := e.facts[idx]
		if (after.IsZero() || f.Timestamp.After(after)) &&
			(before.IsZero() || f.Timestamp.Before(before)) {
			results = append(results, f)
		}
	}

	return results
}

// FactsByPredicate returns matching facts using the index (O(m) instead of O(n)).
func (e *Engine) FactsByPredicate(predicate string) []Fact {
	e.mu.RLock()
	defer e.mu.RUnlock()

	indices, exists := e.index[predicate]
	if !exists {
		return []Fact{}
	}

	results := make([]Fact, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(e.facts) {
			results = append(results, e.facts[idx])
		}
	}

	return results
}

// Facts returns a shallow copy of buffered facts for debugging.
func (e *Engine) Facts() []Fact {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Fact, len(e.facts))
	copy(out, e.facts)
	return out
}

// MatchesAll checks whether every condition has at least one matching fact.
func (e *Engine) MatchesAll(conds []Fact) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, cond := range conds {
		indices, exists := e.index[cond.Predicate]
		if !exists {
			return false
		}

		found := false
		for _, idx := range indices {
			if idx < 0 || idx >= len(e.facts) {
				continue
			}
			f := e.facts[idx]

			if len(cond.Args) == 0 {
				found = true
				break
			}

			if len(f.Args) < len(cond.Args) {
				continue
			}

			ok := true
			for i := range cond.Args {
				if fmt.Sprintf("%v", f.Args[i]) != fmt.Sprintf("%v", cond.Args[i]) {
					ok = false
					break
				}
			}

			if ok {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

// Ready reports whether the engine has a usable query context.
func (e *Engine) Ready() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.schemaLoaded || !e.cfg.Enable
}

// Helper: Convert Fact to Mangle Atom
func (e *Engine) factToAtom(f Fact) (ast.Atom, error) {
	predSym := ast.PredicateSym{Symbol: f.Predicate, Arity: len(f.Args)}

	args := make([]ast.BaseTerm, len(f.Args))
	for i, arg := range f.Args {
		args[i] = e.toConstant(arg)
	}

	return ast.Atom{
		Predicate: predSym,
		Args:      args,
	}, nil
}

// Helper: Convert Mangle Atom to Fact
func (e *Engine) atomToFact(atom ast.Atom) (Fact, error) {
	args := make([]interface{}, len(atom.Args))
	for i, arg := range atom.Args {
		args[i] = e.convertConstant(arg)
	}

	return Fact{
		Predicate: atom.Predicate.Symbol,
		Args:      args,
		Timestamp: time.Now(),
	}, nil
}

// Helper: Convert Go value to Mangle Constant
func (e *Engine) toConstant(v interface{}) ast.Constant {
	switch val := v.(type) {
	case string:
		return ast.String(val)
	case int:
		return ast.Number(int64(val))
	case int64:
		return ast.Number(val)
	case float64:
		return ast.Float64(val)
	case bool:
		if val {
			return ast.String("true")
		}
		return ast.String("false")
	default:
		return ast.String(fmt.Sprintf("%v", v))
	}
}

// Helper: Convert Mangle Constant to Go value
func (e *Engine) convertConstant(c ast.BaseTerm) interface{} {
	if c == nil {
		return nil
	}

	// Handle lazy constants that are returned as functions
	if fn, ok := interface{}(c).(func() (string, error)); ok {
		val, err := fn()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return val
	}

	switch term := c.(type) {
	case ast.Constant:
		// In this version of Mangle, StringValue is a function returning (string, error)
		if term.Type == ast.StringType {
			val, _ := term.StringValue()
			return val
		} else if term.Type == ast.NumberType {
			return term.NumberValue
		} else if term.Type == ast.Float64Type {
			if val, err := term.Float64Value(); err == nil {
				return val
			}
		}
		return term.String()
	case ast.Variable:
		return term.Symbol
	default:
		return fmt.Sprintf("%v", c)
	}
}

// Helper: Rebuild predicate index after circular buffer trim
func (e *Engine) rebuildIndex() {
	e.index = make(map[string][]int)
	for i, f := range e.facts {
		e.index[f.Predicate] = append(e.index[f.Predicate], i)
	}
}
