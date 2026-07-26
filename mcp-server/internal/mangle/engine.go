package mangle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/config"

	"codeberg.org/TauCeti/mangle-go/analysis"
	"codeberg.org/TauCeti/mangle-go/ast"
	"codeberg.org/TauCeti/mangle-go/engine"
	"codeberg.org/TauCeti/mangle-go/factstore"
	"codeberg.org/TauCeti/mangle-go/parse"
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
	store       *factstore.TemporalStore       // Eternal facts
	tempStore   *factstore.TeeingTemporalStore // Active temporal + derived facts
	schemaUnits []parse.SourceUnit
	ruleUnits   []parse.SourceUnit

	// Fact buffer for temporal queries
	facts []Fact

	// Predicate index for O(m) lookup instead of O(n)
	index map[string][]int

	// Adaptive sampling state (PRD Section 3.5)
	samplingRate       float64         // Current sampling rate (1.0 = accept all)
	predicateCounts    map[string]int  // Count of facts per predicate in current window
	lowValuePredicates map[string]bool // Predicates considered low-value for sampling

	// Watch mode subscriptions (PRD Section 5.2)
	subscriptions map[string][]chan WatchEvent   // predicate -> list of subscriber channels
	subMu         sync.RWMutex                   // Separate mutex for subscription management
	watchSeen     map[string]map[string]struct{} // predicate -> fact fingerprints observed after subscription
	evalSlot      chan struct{}                  // prevents timed-out evaluations from piling up
}

// WatchEvent is emitted when a watched predicate derives new facts.
type WatchEvent struct {
	Predicate string    `json:"predicate"`
	Facts     []Fact    `json:"facts"`
	Timestamp time.Time `json:"timestamp"`
}

func NewEngine(cfg config.MangleConfig) (*Engine, error) {
	baseStore := factstore.NewTemporalStore()
	e := &Engine{
		cfg:                cfg,
		facts:              make([]Fact, 0, cfg.FactBufferLimit),
		index:              make(map[string][]int),
		store:              baseStore,
		tempStore:          factstore.NewTeeingTemporalStore(baseStore),
		schemaUnits:        make([]parse.SourceUnit, 0, 1),
		ruleUnits:          make([]parse.SourceUnit, 0),
		samplingRate:       1.0,
		predicateCounts:    make(map[string]int),
		lowValuePredicates: defaultLowValuePredicates(),
		subscriptions:      make(map[string][]chan WatchEvent),
		watchSeen:          make(map[string]map[string]struct{}),
		evalSlot:           make(chan struct{}, 1),
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
	schemaSources, err := discoverSchemaSources(path)
	if err != nil {
		return err
	}

	sourceUnits := make([]parse.SourceUnit, 0, len(schemaSources))
	for _, source := range schemaSources {
		sourceUnit, parseErr := parse.Unit(bytes.NewReader(source.Data))
		if parseErr != nil {
			return fmt.Errorf("parse schema module %s: %w", source.Path, parseErr)
		}
		sourceUnits = append(sourceUnits, sourceUnit)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.schemaUnits = sourceUnits
	e.ruleUnits = nil

	if err := e.rebuildProgramInfoLocked(); err != nil {
		return fmt.Errorf("analyze schema: %w", err)
	}

	e.rebuildTempStoreLocked()

	return nil
}

// getEvalOptions constructs the unified configuration for Mangle evaluation
// against the engine's live temporal store.
func (e *Engine) getEvalOptions() []engine.EvalOption {
	return e.evalOptions(e.tempStore)
}

// evalOptions builds the Mangle evaluation configuration, wiring ts as the
// temporal store when non-nil. Passing an isolated store keeps a read-only
// evaluation (e.g. a conditional query) from touching engine state.
func (e *Engine) evalOptions(ts *factstore.TeeingTemporalStore) []engine.EvalOption {
	extPreds := map[ast.PredicateSym]engine.ExternalPredicateCallback{
		{Symbol: "my_distance", Arity: 5}: DistanceCallback{},
	}

	opts := []engine.EvalOption{
		engine.WithEvaluationTime(time.Now()),
		engine.WithExternalPredicates(extPreds),
		engine.WithCreatedFactLimit(e.cfg.GetMaxCreatedFacts()),
	}

	if ts != nil {
		opts = append(opts, engine.WithTemporalStore(ts))
	}

	return opts
}

func (e *Engine) evalProgramSafe(store factstore.FactStore, phase string) error {
	return e.evalProgramWith(e.programInfo, store, e.tempStore, phase)
}

// evalProgramWith evaluates an explicit program into store, using ts as the
// temporal backing. It recovers panics from the third-party engine so a
// malformed program surfaces as an error instead of crashing the process.
func (e *Engine) evalProgramWith(pi *analysis.ProgramInfo, store factstore.FactStore, ts *factstore.TeeingTemporalStore, phase string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("mangle eval panic during %s: %v\n%s", phase, r, debug.Stack())
		}
	}()
	return engine.EvalProgram(pi, store, e.evalOptions(ts)...)
}

// buildTeeingStoreLocked constructs a fresh temporal store seeded from the
// eternal store and the in-memory fact buffer (including the mt_ mirrors and
// interval coalescing) WITHOUT evaluating any rules or mutating engine state.
// Callers own the subsequent evaluation. e.mu must be held.
func (e *Engine) buildTeeingStoreLocked() *factstore.TeeingTemporalStore {
	ts := factstore.NewTeeingTemporalStore(e.store)
	activePredicates := make(map[ast.PredicateSym]bool)

	for _, f := range e.facts {
		atom, err := e.factToAtom(f)
		if err == nil && !f.Timestamp.IsZero() {
			_, _ = ts.Add(atom, ast.NewPointInterval(f.Timestamp))
			activePredicates[atom.Predicate] = true

			// Create a temporal specific copy with mt_ prefix for DatalogMTL reasoning rules
			mtAtom := atom
			mtAtom.Predicate.Symbol = "mt_" + atom.Predicate.Symbol
			_, _ = ts.Add(mtAtom, ast.NewPointInterval(f.Timestamp))
			activePredicates[mtAtom.Predicate] = true
		}
	}

	// Native Interval Coalescing
	// Merge overlapping consecutive facts (e.g. `loading`=true at T1, T2, T3 -> True [T1, T3])
	for predSym := range activePredicates {
		_ = ts.Coalesce(predSym)
	}

	return ts
}

// rebuildTempStoreLocked recreates the active temporal store from the eternal store + buffer.
func (e *Engine) rebuildTempStoreLocked() {
	e.tempStore = e.buildTeeingStoreLocked()

	if e.schemaLoaded && e.programInfo != nil {
		evalStore := factstore.NewTemporalFactStoreAdapter(e.tempStore)
		if err := e.evalProgramSafe(evalStore, "rebuild_temp_store"); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] %v\n", err)
		}
	}
}

// AddRule dynamically adds a Mangle rule to the program for runtime assertions.
// This enables the PRD's "submit logic rules for continuous evaluation" vision.
func (e *Engine) AddRule(ruleSource string) error {
	if !e.cfg.Enable {
		return nil
	}

	if len(ruleSource) > e.cfg.GetMaxRuleBytes() {
		return fmt.Errorf("rule exceeds max_rule_bytes (%d > %d)", len(ruleSource), e.cfg.GetMaxRuleBytes())
	}

	// Parse the rule
	sourceUnit, err := parse.Unit(bytes.NewReader([]byte(ruleSource)))
	if err != nil {
		return fmt.Errorf("parse rule: %w", err)
	}

	if err := e.validateSourceComplexity(sourceUnit, e.cfg.GetMaxRuleClauses(), e.cfg.GetMaxPremises()); err != nil {
		return fmt.Errorf("rule complexity: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.ruleUnits) >= e.cfg.GetMaxRules() {
		return fmt.Errorf("rule limit reached (%d)", e.cfg.GetMaxRules())
	}

	candidateRules := append(append([]parse.SourceUnit(nil), e.ruleUnits...), sourceUnit)
	units := make([]parse.SourceUnit, 0, len(e.schemaUnits)+len(candidateRules))
	units = append(units, e.schemaUnits...)
	units = append(units, candidateRules...)
	candidateProgram, err := analysis.Analyze(units, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return fmt.Errorf("analyze rule: %w", err)
	}

	// Evaluate the untrusted rule against an isolated store. A timed-out
	// evaluation may finish in the background, but it cannot mutate live facts.
	isolatedBase, isolated := e.buildIsolatedTeeingStoreLocked()
	evalStore := factstore.NewTemporalFactStoreAdapter(isolated)
	if err := e.evalProgramWithTimeout(context.Background(), candidateProgram, evalStore, isolated, "add_rule"); err != nil {
		return fmt.Errorf("eval program after rule insertion: %w", err)
	}

	e.ruleUnits = candidateRules
	e.programInfo = candidateProgram
	e.schemaLoaded = true
	e.store = isolatedBase
	e.tempStore = isolated
	e.checkAndNotifyWatchers(evalStore)
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

	// Adaptive sampling: adjust rate based on buffer pressure and incoming burst size.
	e.updateSamplingRate(len(facts))

	// Filter facts through adaptive sampling
	filtered := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if e.shouldAcceptFact(f) {
			filtered = append(filtered, f)
			e.predicateCounts[f.Predicate]++
		}
	}
	if len(e.ruleUnits) > 0 {
		return e.addFactsWithDynamicRulesLocked(ctx, filtered)
	}

	// Add to temporal buffer with circular buffering
	baseIdx := len(e.facts)
	e.facts = append(e.facts, filtered...)

	needsRebuild := false
	if e.cfg.FactBufferLimit > 0 && len(e.facts) > e.cfg.FactBufferLimit {
		trimCount := len(e.facts) - e.cfg.FactBufferLimit
		e.facts = e.facts[trimCount:]
		baseIdx -= trimCount

		// Rebuild index after trim
		e.rebuildIndex()
		needsRebuild = true
	} else {
		// Incremental index update
		for i, f := range filtered {
			idx := baseIdx + i
			if idx >= 0 && idx < len(e.facts) {
				e.index[f.Predicate] = append(e.index[f.Predicate], idx)
			}
		}
	}

	if needsRebuild {
		// Preserve eternal facts from this batch before rebuilding temporal state.
		e.addEternalFactsLocked(filtered)
		e.rebuildTempStoreLocked()
	} else {
		// Add to Mangle store for rule evaluation incrementally
		for _, f := range filtered {
			atom, err := e.factToAtom(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] factToAtom failed for %s: %v\n", f.Predicate, err)
				continue // Skip malformed facts
			}
			if f.Timestamp.IsZero() {
				added, _ := e.store.AddEternal(atom)
				if added {
					fmt.Fprintf(os.Stderr, "[DEBUG] Added eternal fact to store: %s (Arity: %d)\n", f.Predicate, len(f.Args))
				}
			} else {
				added, _ := e.tempStore.Add(atom, ast.NewPointInterval(f.Timestamp))
				if added {
					fmt.Fprintf(os.Stderr, "[DEBUG] Added temporal fact to store: %s (Arity: %d)\n", f.Predicate, len(f.Args))
				}
				// Mirror temporal facts with mt_ prefix for DatalogMTL rules in incremental mode.
				mtAtom := atom
				mtAtom.Predicate.Symbol = "mt_" + atom.Predicate.Symbol
				_, _ = e.tempStore.Add(mtAtom, ast.NewPointInterval(f.Timestamp))
			}
		}

		// Trigger incremental evaluation if schema loaded
		if e.schemaLoaded && e.programInfo != nil {
			evalStore := factstore.NewTemporalFactStoreAdapter(e.tempStore)
			if err := e.evalProgramSafe(evalStore, "add_facts"); err != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] EvalProgram failed: %v\n", err)
				return fmt.Errorf("eval program after fact insertion: %w", err)
			}
		}
	}

	if e.schemaLoaded && e.programInfo != nil {
		evalStore := factstore.NewTemporalFactStoreAdapter(e.tempStore)
		// Check watched predicates and notify subscribers (Watch Mode - PRD 5.2)
		e.checkAndNotifyWatchers(evalStore)
	}

	return nil
}

// addFactsWithDynamicRulesLocked evaluates user-extended programs
// transactionally. A timeout can leave third-party evaluation running, so the
// candidate store is isolated and the live fact buffer/store are swapped only
// after successful completion. Caller must hold e.mu.
func (e *Engine) addFactsWithDynamicRulesLocked(ctx context.Context, filtered []Fact) error {
	previousFacts := e.facts
	candidateFacts := make([]Fact, 0, len(previousFacts)+len(filtered))
	candidateFacts = append(candidateFacts, previousFacts...)
	candidateFacts = append(candidateFacts, filtered...)
	if limit := e.cfg.FactBufferLimit; limit > 0 && len(candidateFacts) > limit {
		candidateFacts = candidateFacts[len(candidateFacts)-limit:]
	}

	e.facts = candidateFacts
	candidateBase, candidateStore := e.buildIsolatedTeeingStoreLocked()
	e.facts = previousFacts

	evalStore := factstore.NewTemporalFactStoreAdapter(candidateStore)
	if err := e.evalProgramWithTimeout(ctx, e.programInfo, evalStore, candidateStore, "add_facts_dynamic"); err != nil {
		for _, fact := range filtered {
			if e.predicateCounts[fact.Predicate] > 0 {
				e.predicateCounts[fact.Predicate]--
			}
		}
		return fmt.Errorf("eval dynamic program after fact insertion: %w", err)
	}

	e.facts = candidateFacts
	e.rebuildIndex()
	e.store = candidateBase
	e.tempStore = candidateStore
	e.checkAndNotifyWatchers(evalStore)
	return nil
}

// checkAndNotifyWatchers evaluates watched predicates and notifies subscribers.
func (e *Engine) checkAndNotifyWatchers(store factstore.FactStore) {
	watchedPredicates := e.WatchPredicates()
	if len(watchedPredicates) == 0 {
		return
	}

	for _, predicate := range watchedPredicates {
		// Query the store for derived facts using the declared arity when available.
		arity := e.predicateArityLocked(predicate)
		predSym := ast.PredicateSym{Symbol: predicate, Arity: arity}
		wildcardAtom := ast.Atom{Predicate: predSym}
		if arity >= 0 {
			args := make([]ast.BaseTerm, arity)
			for i := 0; i < arity; i++ {
				args[i] = ast.Variable{Symbol: fmt.Sprintf("W%d", i)}
			}
			wildcardAtom.Args = args
		}

		var derivedFacts []Fact
		_ = store.GetFacts(wildcardAtom, func(atom ast.Atom) error {
			fact, err := e.atomToFact(atom)
			if err == nil {
				derivedFacts = append(derivedFacts, fact)
			}
			return nil
		})

		if fresh := e.filterNewWatchFacts(predicate, derivedFacts); len(fresh) > 0 {
			e.notifySubscribers(predicate, fresh)
		}
	}
}

func (e *Engine) predicateArityLocked(predicate string) int {
	if e.programInfo == nil {
		if indices, ok := e.index[predicate]; ok && len(indices) > 0 {
			first := indices[0]
			if first >= 0 && first < len(e.facts) {
				return len(e.facts[first].Args)
			}
		}
		return -1
	}
	for sym := range e.programInfo.Decls {
		if sym.Symbol == predicate {
			return sym.Arity
		}
	}
	if indices, ok := e.index[predicate]; ok && len(indices) > 0 {
		first := indices[0]
		if first >= 0 && first < len(e.facts) {
			return len(e.facts[first].Args)
		}
	}
	return -1
}

// updateSamplingRate adjusts sampling based on buffer pressure (PRD Section 3.5).
// When buffer is >80% full, start dropping low-value facts.
func (e *Engine) updateSamplingRate(incomingCount int) {
	if e.cfg.FactBufferLimit <= 0 {
		e.samplingRate = 1.0
		return
	}

	fillRatio := float64(len(e.facts)) / float64(e.cfg.FactBufferLimit)
	if fillRatio < 0 {
		fillRatio = 0
	}
	if fillRatio > 1 {
		fillRatio = 1
	}

	burstRatio := 0.0
	if incomingCount > 0 {
		burstRatio = float64(incomingCount) / float64(e.cfg.FactBufferLimit)
	}

	switch {
	case fillRatio < 0.5:
		e.samplingRate = 1.0 // Accept all
	case fillRatio < 0.7:
		e.samplingRate = 0.9 // Drop 10% of low-value
	case fillRatio < 0.85:
		e.samplingRate = 0.75 // Drop 25% of low-value
	case fillRatio < 0.95:
		switch {
		case burstRatio < 0.02:
			e.samplingRate = 0.9
		case burstRatio < 0.05:
			e.samplingRate = 0.7
		default:
			e.samplingRate = 0.4
		}
	default:
		switch {
		case burstRatio < 0.01:
			e.samplingRate = 0.85
		case burstRatio < 0.05:
			e.samplingRate = 0.6
		case burstRatio < 0.1:
			e.samplingRate = 0.3
		default:
			e.samplingRate = 0.1 // Drop 90% only for true bursts while saturated.
		}
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
	e.mu.RLock()
	baseline := e.currentPredicateFingerprintsLocked(predicate)
	e.subMu.Lock()
	if len(e.subscriptions[predicate]) == 0 {
		e.watchSeen[predicate] = baseline
	}
	e.subscriptions[predicate] = append(e.subscriptions[predicate], ch)
	e.subMu.Unlock()
	e.mu.RUnlock()
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
			next := make([]chan WatchEvent, 0, len(channels)-1)
			next = append(next, channels[:i]...)
			next = append(next, channels[i+1:]...)
			if len(next) == 0 {
				delete(e.subscriptions, predicate)
				delete(e.watchSeen, predicate)
			} else {
				e.subscriptions[predicate] = next
			}
			break
		}
	}
}

// notifySubscribers sends events to all subscribers watching a predicate.
// Called after rule evaluation when new facts are derived.
func (e *Engine) notifySubscribers(predicate string, facts []Fact) {
	e.subMu.RLock()
	channels := append([]chan WatchEvent(nil), e.subscriptions[predicate]...)
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

	queryStr = strings.TrimSpace(queryStr)
	if queryStr == "" {
		return nil, fmt.Errorf("query is required")
	}
	if len(queryStr) > e.cfg.GetMaxQueryBytes() {
		return nil, fmt.Errorf("query exceeds max_query_bytes (%d > %d)", len(queryStr), e.cfg.GetMaxQueryBytes())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(queryStr, ".") {
		queryStr += "."
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
	if len(sourceUnit.Clauses) != 1 {
		return nil, fmt.Errorf("query must contain exactly one clause")
	}
	if err := e.validateSourceComplexity(sourceUnit, 1, e.cfg.GetMaxPremises()); err != nil {
		return nil, fmt.Errorf("query complexity: %w", err)
	}

	clause := sourceUnit.Clauses[0]

	e.mu.RLock()
	defer e.mu.RUnlock()

	// A query with a body (premises) is a conditional query: its head projects
	// the variables that satisfy the body. Evaluate the body through Mangle
	// rather than pattern-matching the head against stored facts, which would
	// silently ignore the conditions (e.g. `T > 3000`).
	if len(clause.Premises) > 0 {
		return e.queryWithBodyLocked(ctx, clause)
	}

	// In Mangle Go v0.5.0, a body-less query is a Clause with a Head atom.
	queryAtom := clause.Head
	queryAtom = normalizeAnonymousVariables(queryAtom)

	// Get all facts matching the query predicate using callback pattern
	results := make([]QueryResult, 0)

	evalStore := factstore.NewTemporalFactStoreAdapter(e.tempStore)
	err = evalStore.GetFacts(queryAtom, func(atom ast.Atom) error {
		if len(results) >= e.cfg.GetMaxQueryResults() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result := make(QueryResult)
		matched := true

		// Bind variables from the query to fact arguments
		for i, arg := range queryAtom.Args {
			if i >= len(atom.Args) {
				matched = false
				break
			}

			val := e.convertConstant(atom.Args[i])
			if varArg, ok := arg.(ast.Variable); ok {
				if existing, exists := result[varArg.Symbol]; exists && !valuesEquivalent(existing, val) {
					matched = false
					break
				}
				result[varArg.Symbol] = val
				continue
			}

			if constArg, ok := arg.(ast.Constant); ok {
				if !valuesEquivalent(e.convertConstant(constArg), val) {
					matched = false
					break
				}
			}
		}

		if matched {
			results = append(results, result)
		}
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
	if len(results) > e.cfg.GetMaxQueryResults() {
		results = results[:e.cfg.GetMaxQueryResults()]
	}

	return results, nil
}

// queryResultPredicate is the synthetic head predicate used to project the
// results of a conditional query. It is namespaced to avoid colliding with
// user or schema predicates.
const queryResultPredicate = "__browsernerd_query_result"

// queryWithBodyLocked evaluates a conditional query (a clause with premises) by
// projecting its body onto a synthetic head predicate and running full Mangle
// evaluation over an isolated store. This keeps the query read-only; it never
// mutates the engine's live program or temporal store while still enforcing
// the body's conditions, negation, and comparisons. e.mu must be held.
func (e *Engine) queryWithBodyLocked(ctx context.Context, clause ast.Clause) ([]QueryResult, error) {
	if !e.schemaLoaded || e.programInfo == nil {
		return nil, fmt.Errorf("engine not ready")
	}

	// Project the body onto a fresh predicate carrying the original head's args.
	headArgs := clause.Head.Args
	synthSym := ast.PredicateSym{Symbol: queryResultPredicate, Arity: len(headArgs)}
	synthClause := ast.Clause{
		Head:      ast.Atom{Predicate: synthSym, Args: headArgs},
		Premises:  clause.Premises,
		Transform: clause.Transform,
	}

	// Analyze schema + accumulated rules + the synthetic projection into a fresh
	// program. This does not touch e.programInfo.
	units := make([]parse.SourceUnit, 0, len(e.schemaUnits)+len(e.ruleUnits)+1)
	units = append(units, e.schemaUnits...)
	units = append(units, e.ruleUnits...)
	units = append(units, parse.SourceUnit{Clauses: []ast.Clause{synthClause}})

	programInfo, err := analysis.Analyze(units, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return nil, fmt.Errorf("analyze query: %w", err)
	}

	// Evaluate into a fully isolated copy of the current facts.
	_, isolated := e.buildIsolatedTeeingStoreLocked()
	evalStore := factstore.NewTemporalFactStoreAdapter(isolated)
	if err := e.evalProgramWithTimeout(ctx, programInfo, evalStore, isolated, "query_body"); err != nil {
		return nil, fmt.Errorf("evaluate query: %w", err)
	}

	// Read out the derived projection, mapping each argument position back to the
	// variable named in the original head.
	wildcards := make([]ast.BaseTerm, len(headArgs))
	for i := range wildcards {
		wildcards[i] = ast.Variable{Symbol: fmt.Sprintf("Q%d", i)}
	}

	results := make([]QueryResult, 0)
	err = evalStore.GetFacts(ast.Atom{Predicate: synthSym, Args: wildcards}, func(atom ast.Atom) error {
		if len(results) >= e.cfg.GetMaxQueryResults() {
			return nil
		}
		result := make(QueryResult)
		for i, arg := range headArgs {
			if i >= len(atom.Args) {
				break
			}
			if varArg, ok := arg.(ast.Variable); ok && varArg.Symbol != "_" {
				result[varArg.Symbol] = e.convertConstant(atom.Args[i])
			}
		}
		results = append(results, result)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read query results: %w", err)
	}

	return results, nil
}

func (e *Engine) evalProgramWithTimeout(ctx context.Context, pi *analysis.ProgramInfo, store factstore.FactStore, ts *factstore.TeeingTemporalStore, phase string) error {
	timeout := e.cfg.GetEvaluationTimeout()
	evalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case e.evalSlot <- struct{}{}:
	default:
		return fmt.Errorf("mangle evaluation busy during %s", phase)
	}
	errCh := make(chan error, 1)
	go func() {
		defer func() { <-e.evalSlot }()
		errCh <- e.evalProgramWith(pi, store, ts, phase)
	}()
	select {
	case err := <-errCh:
		return err
	case <-evalCtx.Done():
		return fmt.Errorf("mangle evaluation timed out during %s after %s: %w", phase, timeout, evalCtx.Err())
	}
}

func (e *Engine) buildIsolatedTeeingStoreLocked() (*factstore.TemporalStore, *factstore.TeeingTemporalStore) {
	base := factstore.NewTemporalStore()
	ts := factstore.NewTeeingTemporalStore(base)
	activePredicates := make(map[ast.PredicateSym]bool)
	for _, fact := range e.facts {
		atom, err := e.factToAtom(fact)
		if err != nil {
			continue
		}
		if fact.Timestamp.IsZero() {
			_, _ = base.AddEternal(atom)
			continue
		}
		_, _ = ts.Add(atom, ast.NewPointInterval(fact.Timestamp))
		activePredicates[atom.Predicate] = true
		mtAtom := atom
		mtAtom.Predicate.Symbol = "mt_" + atom.Predicate.Symbol
		_, _ = ts.Add(mtAtom, ast.NewPointInterval(fact.Timestamp))
		activePredicates[mtAtom.Predicate] = true
	}
	for predicate := range activePredicates {
		_ = ts.Coalesce(predicate)
	}
	return base, ts
}

func (e *Engine) validateSourceComplexity(unit parse.SourceUnit, maxClauses, maxPremises int) error {
	if len(unit.Clauses) > maxClauses {
		return fmt.Errorf("clause limit exceeded (%d > %d)", len(unit.Clauses), maxClauses)
	}
	totalPremises := 0
	for _, clause := range unit.Clauses {
		totalPremises += len(clause.Premises)
	}
	if totalPremises > maxPremises {
		return fmt.Errorf("premise limit exceeded (%d > %d)", totalPremises, maxPremises)
	}
	return nil
}

func (e *Engine) currentPredicateFingerprintsLocked(predicate string) map[string]struct{} {
	seen := make(map[string]struct{})
	if e.tempStore == nil {
		return seen
	}
	arity := e.predicateArityLocked(predicate)
	query := ast.Atom{Predicate: ast.PredicateSym{Symbol: predicate, Arity: arity}}
	if arity >= 0 {
		query.Args = make([]ast.BaseTerm, arity)
		for i := range query.Args {
			query.Args[i] = ast.Variable{Symbol: fmt.Sprintf("B%d", i)}
		}
	}
	store := factstore.NewTemporalFactStoreAdapter(e.tempStore)
	_ = store.GetFacts(query, func(atom ast.Atom) error {
		if fact, err := e.atomToFact(atom); err == nil {
			seen[factFingerprint(fact)] = struct{}{}
		}
		return nil
	})
	return seen
}

func (e *Engine) filterNewWatchFacts(predicate string, facts []Fact) []Fact {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	seen, ok := e.watchSeen[predicate]
	if !ok {
		seen = make(map[string]struct{})
		e.watchSeen[predicate] = seen
	}
	fresh := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		key := factFingerprint(fact)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		fresh = append(fresh, fact)
	}
	return fresh
}

func factFingerprint(fact Fact) string {
	encoded, err := json.Marshal(fact.Args)
	if err != nil {
		return fact.Predicate + ":" + fmt.Sprint(fact.Args)
	}
	return fact.Predicate + ":" + string(encoded)
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
				matches = false
				break
			}

			if varArg, ok := qArg.(ast.Variable); ok {
				if existing, exists := result[varArg.Symbol]; exists {
					if !valuesEquivalent(existing, f.Args[i]) {
						matches = false
						break
					}
				} else {
					result[varArg.Symbol] = f.Args[i]
				}
			} else if constArg, ok := qArg.(ast.Constant); ok {
				if !valuesEquivalent(e.convertConstant(constArg), f.Args[i]) {
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
	evalStore := factstore.NewTemporalFactStoreAdapter(e.tempStore)
	if err := e.evalProgramSafe(evalStore, "evaluate"); err != nil {
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

	err := evalStore.GetFacts(queryAtom, func(atom ast.Atom) error {
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
	if e.tempStore == nil || (!e.schemaLoaded) {
		return results
	}

	// 1. Determine Arity from schema Definitions
	arity := -1
	if e.programInfo != nil {
		for sym := range e.programInfo.Decls {
			if sym.Symbol == predicate {
				arity = sym.Arity
				break
			}
		}
	}

	// Fallback to buffered fact length if not in schema (e.g. ad-hoc unit test predicates)
	if arity == -1 {
		if indices, ok := e.index[predicate]; ok && len(indices) > 0 {
			if len(e.facts) > indices[0] {
				arity = len(e.facts[indices[0]].Args)
			}
		}
	}

	if arity == -1 {
		return results // Unknown predicate with no facts
	}

	predSym := ast.PredicateSym{Symbol: predicate, Arity: arity}

	// 2. Build Query Atom with wildcard Variables
	var queryAtom ast.Atom
	if arity >= 0 {
		args := make([]ast.BaseTerm, arity)
		for i := 0; i < arity; i++ {
			args[i] = ast.Variable{Symbol: fmt.Sprintf("V%d", i)}
		}
		queryAtom = ast.Atom{Predicate: predSym, Args: args}
	} else {
		queryAtom = ast.Atom{Predicate: predSym}
	}

	// 3. Construct Temporal Bounds dynamically
	var startBound, endBound ast.TemporalBound
	if after.IsZero() {
		startBound = ast.NegativeInfinity()
	} else {
		startBound = ast.NewTimestampBound(after)
	}

	if before.IsZero() {
		endBound = ast.PositiveInfinity()
	} else {
		endBound = ast.NewTimestampBound(before)
	}

	interval := ast.Interval{Start: startBound, End: endBound}

	// 4. Time-Windowed Fact Extraction API via TeeingTemporalStore
	_ = e.tempStore.GetFactsDuring(queryAtom, interval, func(tf factstore.TemporalFact) error {
		fact, err := e.atomToFact(tf.Atom)
		if err == nil {
			// Attach exact interval start time to the result for correlation context
			if tf.Interval.Start.Type == ast.TimestampBound {
				fact.Timestamp = tf.Interval.Start.Time()
			}
			results = append(results, fact)
		}
		return nil
	})

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
				if fmt.Sprintf("%v", cond.Args[i]) == "_" {
					continue
				}
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

func (e *Engine) addEternalFactsLocked(facts []Fact) {
	for _, f := range facts {
		if !f.Timestamp.IsZero() {
			continue
		}
		atom, err := e.factToAtom(f)
		if err != nil {
			continue
		}
		_, _ = e.store.AddEternal(atom)
	}
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
			// ast.Constant.NumberValue is a method; returning it (without calling) leaks a func() (int64, error)
			// into query bindings, which then breaks JSON marshaling downstream.
			val, _ := term.NumberValue()
			return val
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

func (e *Engine) rebuildProgramInfoLocked() error {
	units := make([]parse.SourceUnit, 0, len(e.schemaUnits)+len(e.ruleUnits))
	units = append(units, e.schemaUnits...)
	units = append(units, e.ruleUnits...)
	if len(units) == 0 {
		return fmt.Errorf("no schema or rules loaded")
	}

	programInfo, err := analysis.Analyze(units, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return err
	}

	e.programInfo = programInfo
	e.schemaLoaded = true
	return nil
}

func normalizeAnonymousVariables(atom ast.Atom) ast.Atom {
	if len(atom.Args) == 0 {
		return atom
	}

	args := make([]ast.BaseTerm, len(atom.Args))
	anonCount := 0
	for i, arg := range atom.Args {
		if varArg, ok := arg.(ast.Variable); ok && varArg.Symbol == "_" {
			args[i] = ast.Variable{Symbol: fmt.Sprintf("__anon_%d", anonCount)}
			anonCount++
			continue
		}
		args[i] = arg
	}

	atom.Args = args
	return atom
}

func valuesEquivalent(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
