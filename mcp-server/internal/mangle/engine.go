package mangle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// WatchEvent is emitted when a watched predicate derives new facts.
type WatchEvent struct {
	Predicate string    `json:"predicate"`
	Facts     []Fact    `json:"facts"`
	Timestamp time.Time `json:"timestamp"`
}

// errNotReady is returned by every read when no program is loaded.
var errNotReady = errors.New("engine not ready")

// watchDebounce coalesces a burst of fact batches into one evaluation for
// subscribers.
const watchDebounce = 50 * time.Millisecond

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
}

// Engine is the Mangle deductive database behind every browser fact.
//
// Writes are cheap: AddFacts appends to a bounded buffer and to an indexed base
// store, and evaluates nothing. Derived facts are computed when something reads
// them (a query, an evaluate, a watch) and reused until the next write, so a
// polling page pays for one evaluation per read instead of one per CDP event.
// Evaluation always starts from the base facts, so a rule with negation never
// keeps a conclusion a later fact refuted.
type Engine struct {
	cfg          config.MangleConfig
	mu           sync.RWMutex
	schemaLoaded bool

	// Program
	programInfo     *analysis.ProgramInfo
	schemaUnits     []parse.SourceUnit
	schemaModules   []schemaModule // parallel to schemaUnits
	declModule      map[string]string
	ruleUnits       []parse.SourceUnit
	temporalMirrors map[string]bool // base predicates whose mt_ copy the program reads

	// Fact buffer and its predicate index.
	facts []Fact
	index map[string][]int
	// Eternal facts (zero timestamp) outlive buffer eviction.
	eternal map[uint64]ast.Atom

	// base holds exactly the buffer's facts (plus mt_ mirrors) while
	// baseFresh. derivations caches, per target set (closureKey), what the
	// rules those targets depend on derive over the current base; any write
	// clears it.
	base        *indexedStore
	baseFresh   bool
	derivations map[string]*indexedStore

	// Adaptive sampling state
	samplingRate       float64
	predicateCounts    map[string]int
	lowValuePredicates map[string]bool

	counters engineCounters

	// Watch mode subscriptions
	subscriptions map[string][]chan WatchEvent
	subMu         sync.RWMutex
	watchSeen     map[string]map[string]struct{}
	watchPending  bool
	evalSlot      chan struct{} // prevents timed-out evaluations from piling up
}

// engineCounters are cumulative engine measurements for Stats.
type engineCounters struct {
	evicted       [signalClassCount]int64
	trims         int64
	superseded    int64
	sampledOut    int64
	lastBuild     time.Duration
	lastEval      time.Duration
	lastEvalError string
	evaluations   int64
}

func NewEngine(cfg config.MangleConfig) (*Engine, error) {
	e := &Engine{
		cfg:                cfg,
		facts:              make([]Fact, 0, cfg.FactBufferLimit),
		index:              make(map[string][]int),
		eternal:            make(map[uint64]ast.Atom),
		declModule:         make(map[string]string),
		temporalMirrors:    make(map[string]bool),
		samplingRate:       1.0,
		predicateCounts:    make(map[string]int),
		lowValuePredicates: defaultLowValuePredicates(),
		subscriptions:      make(map[string][]chan WatchEvent),
		watchSeen:          make(map[string]map[string]struct{}),
		evalSlot:           make(chan struct{}, 1),
	}

	if cfg.Enable {
		sources, err := resolveSchemaSources(!cfg.DisableBuiltin, cfg.SchemaPath)
		if err != nil {
			return nil, err
		}
		if len(sources) > 0 {
			if err := e.loadSchema(sources); err != nil {
				return nil, err
			}
		}
	}

	return e, nil
}

// loadSchema parses and analyzes the program's modules and prepares the engine
// for evaluation. Every predicate is declared in exactly one module: a second
// Decl of the same name is a conflict between modules, and it is reported with
// both modules named rather than left for the analyzer to pick a winner.
func (e *Engine) loadSchema(sources []schemaSource) error {
	sourceUnits := make([]parse.SourceUnit, 0, len(sources))
	modules := make([]schemaModule, 0, len(sources))
	declaredIn := make(map[string]schemaModule)
	for _, source := range sources {
		sourceUnit, parseErr := parse.Unit(bytes.NewReader(source.Data))
		if parseErr != nil {
			return fmt.Errorf("parse schema module %s: %w", source.Path, parseErr)
		}
		module := schemaModule{Name: source.Module(), Origin: source.Origin, Path: source.Path}
		for _, decl := range sourceUnit.Decls {
			name := decl.DeclaredAtom.Predicate.Symbol
			if name == "Package" || name == "Use" {
				continue
			}
			if previous, exists := declaredIn[name]; exists {
				return fmt.Errorf("predicate %s is declared in both %s and %s: a project module may add rules to a built-in predicate but must not redeclare it", name, previous.Label(), module.Label())
			}
			declaredIn[name] = module
		}
		sourceUnits = append(sourceUnits, sourceUnit)
		modules = append(modules, module)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.schemaUnits = sourceUnits
	e.schemaModules = modules
	e.ruleUnits = nil
	e.declModule = make(map[string]string, len(declaredIn))
	for name, module := range declaredIn {
		e.declModule[name] = module.Label()
	}

	if err := e.rebuildProgramInfoLocked(); err != nil {
		return fmt.Errorf("analyze schema: %w", err)
	}
	return nil
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
	e.setProgramLocked(programInfo)
	return nil
}

// setProgramLocked installs an analyzed program. The base store is rebuilt on
// the next read because the set of mirrored temporal predicates may change.
func (e *Engine) setProgramLocked(programInfo *analysis.ProgramInfo) {
	e.programInfo = programInfo
	e.schemaLoaded = true
	e.temporalMirrors = temporalMirrorsOf(programInfo)
	e.baseFresh = false
	e.derivations = nil
}

// temporalMirrorsOf lists the base predicates whose mt_ temporal copy the
// program references; only those are mirrored into the store.
func temporalMirrorsOf(programInfo *analysis.ProgramInfo) map[string]bool {
	mirrors := make(map[string]bool)
	if programInfo == nil {
		return mirrors
	}
	for sym := range programInfo.Decls {
		if strings.HasPrefix(sym.Symbol, "mt_") {
			mirrors[strings.TrimPrefix(sym.Symbol, "mt_")] = true
		}
	}
	return mirrors
}

// debugf logs engine internals only when BROWSERNERD_DEBUG=1. Per-fact output
// on stderr once filled a driver's unread pipe and hung every CDP call.
func debugf(format string, args ...interface{}) {
	if os.Getenv("BROWSERNERD_DEBUG") == "1" {
		log.Printf("[mangle] "+format, args...)
	}
}

// evalOptions builds the Mangle evaluation configuration, wiring ts as the
// temporal store when non-nil.
func (e *Engine) evalOptions(ts factstore.TemporalFactStore) []engine.EvalOption {
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

// evalProgramWith evaluates an explicit program into store, using ts as the
// temporal backing. It recovers panics from the third-party engine so a
// malformed program surfaces as an error instead of crashing the process.
func (e *Engine) evalProgramWith(pi *analysis.ProgramInfo, store factstore.FactStore, ts factstore.TemporalFactStore, phase string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("mangle eval panic during %s: %v\n%s", phase, r, debug.Stack())
		}
	}()
	_, err = engine.EvalProgramWithStats(pi, store, e.evalOptions(ts)...)
	return err
}

func (e *Engine) evalProgramWithTimeout(ctx context.Context, pi *analysis.ProgramInfo, store factstore.FactStore, ts factstore.TemporalFactStore, phase string) error {
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

// buildBaseLocked builds the indexed base store from the eternal facts and the
// buffer, with mt_ mirrors for the predicates the program reads temporally.
func (e *Engine) buildBaseLocked() *indexedStore {
	s := newIndexedStore()
	for _, atom := range e.eternal {
		_, _ = s.AddEternal(atom)
	}
	mirrored := make(map[ast.PredicateSym]bool)
	for _, f := range e.facts {
		if sym, ok := e.addToBase(s, f); ok {
			mirrored[sym] = true
		}
	}
	// Merge a mirrored fact's adjacent points (loading=true at T1, T2, T3
	// becomes true over [T1, T3]) for DatalogMTL rules.
	for sym := range mirrored {
		_ = s.Coalesce(sym)
	}
	return s
}

// addToBase adds one fact (and its mt_ mirror when the program reads one) and
// reports the mirror's symbol.
func (e *Engine) addToBase(s *indexedStore, f Fact) (ast.PredicateSym, bool) {
	atom := e.factToAtom(f)
	if f.Timestamp.IsZero() {
		_, _ = s.AddEternal(atom)
		return ast.PredicateSym{}, false
	}
	interval := ast.NewPointInterval(f.Timestamp)
	_, _ = s.Add(atom, interval)
	if !e.temporalMirrors[f.Predicate] {
		return ast.PredicateSym{}, false
	}
	mirror := atom
	mirror.Predicate.Symbol = "mt_" + atom.Predicate.Symbol
	_, _ = s.Add(mirror, interval)
	return mirror.Predicate, true
}

// ensureBaseLocked makes the base store match the buffer.
func (e *Engine) ensureBaseLocked() {
	if e.baseFresh && e.base != nil {
		return
	}
	start := time.Now()
	e.base = e.buildBaseLocked()
	e.baseFresh = true
	e.derivations = nil
	e.counters.lastBuild = time.Since(start)
}

// derivedForLocked returns what the rules the targets depend on derive over
// the current base (all rules for no targets), evaluating them unless a cached
// result is still current. A failed or timed-out evaluation leaves nothing
// behind: the base is rebuilt before the next evaluation, so one still running
// in the background never shares a store with later writes.
func (e *Engine) derivedForLocked(ctx context.Context, targets []string) (*indexedStore, error) {
	if !e.schemaLoaded || e.programInfo == nil {
		return nil, errNotReady
	}
	e.ensureBaseLocked()
	if full := e.derivations[closureKey(nil)]; full != nil {
		return full, nil
	}
	key := closureKey(targets)
	if cached := e.derivations[key]; cached != nil {
		return cached, nil
	}
	program := closureProgram(e.programInfo, targets)
	derived := newIndexedStore()
	if len(program.Rules) > 0 || len(program.InitialFacts) > 0 {
		overlay := &overlayStore{base: e.base, out: derived}
		start := time.Now()
		err := e.evalProgramWithTimeout(ctx, program, factstore.NewTemporalFactStoreAdapter(overlay), overlay, "derive "+key)
		e.counters.lastEval = time.Since(start)
		e.counters.evaluations++
		if err != nil {
			e.counters.lastEvalError = err.Error()
			e.derivations = nil
			e.baseFresh = false
			debugf("evaluation of %s failed: %v", key, err)
			return nil, err
		}
		e.counters.lastEvalError = ""
	}
	if e.derivations == nil {
		e.derivations = make(map[string]*indexedStore)
	}
	e.derivations[key] = derived
	return derived, nil
}

// readStoreLocked returns the store to read predicate from: the base store
// alone for a base predicate, base plus its derivations for a derived one.
func (e *Engine) readStoreLocked(ctx context.Context, predicate string) (factstore.TemporalFactStore, error) {
	if e.isDerivedLocked(predicate) {
		derived, err := e.derivedForLocked(ctx, []string{predicate})
		if err != nil {
			return nil, err
		}
		return &overlayStore{base: e.base, out: derived}, nil
	}
	e.ensureBaseLocked()
	return e.base, nil
}

// isDerivedLocked reports whether some rule derives predicate.
func (e *Engine) isDerivedLocked(predicate string) bool {
	if e.programInfo == nil {
		return false
	}
	for sym := range e.programInfo.IdbPredicates {
		if sym.Symbol == predicate {
			return true
		}
	}
	return false
}

// AddRule dynamically adds a Mangle rule to the program for runtime assertions.
// The rule is analyzed with the whole program and evaluated once over the
// current facts, bounded by the evaluation timeout, before it is accepted.
func (e *Engine) AddRule(ruleSource string) error {
	if !e.cfg.Enable {
		return nil
	}

	if len(ruleSource) > e.cfg.GetMaxRuleBytes() {
		return fmt.Errorf("rule exceeds max_rule_bytes (%d > %d)", len(ruleSource), e.cfg.GetMaxRuleBytes())
	}

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
	if err := e.checkRuleUnitLocked(sourceUnit); err != nil {
		return err
	}

	candidateRules := append(append([]parse.SourceUnit(nil), e.ruleUnits...), sourceUnit)
	units := make([]parse.SourceUnit, 0, len(e.schemaUnits)+len(candidateRules))
	units = append(units, e.schemaUnits...)
	units = append(units, candidateRules...)
	candidateProgram, err := analysis.Analyze(units, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return fmt.Errorf("analyze rule: %w", err)
	}

	// Evaluate what the submitted rules derive - their heads and everything
	// those depend on - over the current facts, into a store nothing else
	// reads. A timed-out evaluation may finish in the background, but it can
	// change nothing live.
	heads := make([]string, 0, len(sourceUnit.Clauses))
	for _, clause := range sourceUnit.Clauses {
		heads = append(heads, clause.Head.Predicate.Symbol)
	}
	previousMirrors := e.temporalMirrors
	e.temporalMirrors = temporalMirrorsOf(candidateProgram)
	if !mirrorsEqual(previousMirrors, e.temporalMirrors) {
		e.baseFresh = false
	}
	e.ensureBaseLocked()
	derived := newIndexedStore()
	overlay := &overlayStore{base: e.base, out: derived}
	if err := e.evalProgramWithTimeout(context.Background(), closureProgram(candidateProgram, heads), factstore.NewTemporalFactStoreAdapter(overlay), overlay, "add_rule"); err != nil {
		e.temporalMirrors = previousMirrors
		e.baseFresh = false
		return fmt.Errorf("eval program after rule insertion: %w", err)
	}

	e.ruleUnits = candidateRules
	for _, decl := range sourceUnit.Decls {
		e.declModule[decl.DeclaredAtom.Predicate.Symbol] = "runtime"
	}
	e.programInfo = candidateProgram
	e.schemaLoaded = true
	e.derivations = map[string]*indexedStore{closureKey(heads): derived}
	e.checkAndNotifyWatchersLocked()
	return nil
}

func mirrorsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// AddFacts appends incoming facts to the buffer. Low-value predicates are
// sampled under pressure; over the limit the buffer sheds low-signal facts
// first (see retention.go). Nothing is evaluated here.
func (e *Engine) AddFacts(ctx context.Context, facts []Fact) error {
	if !e.cfg.Enable {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.updateSamplingRate(len(facts))
	filtered := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if e.shouldAcceptFact(f) {
			filtered = append(filtered, f)
			e.predicateCounts[f.Predicate]++
		} else {
			e.counters.sampledOut++
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	for _, f := range filtered {
		if f.Timestamp.IsZero() {
			atom := e.factToAtom(f)
			e.eternal[atom.Hash()] = atom
		}
	}
	filtered = e.supersedeLocked(filtered)

	baseIdx := len(e.facts)
	e.facts = append(e.facts, filtered...)
	for i, f := range filtered {
		e.index[f.Predicate] = append(e.index[f.Predicate], baseIdx+i)
	}
	if e.baseFresh && e.base != nil {
		for _, f := range filtered {
			e.addToBase(e.base, f)
		}
	}
	if limit := e.cfg.FactBufferLimit; limit > 0 && len(e.facts) > limit {
		e.trimLocked(limit)
	}
	e.derivations = nil

	if len(e.WatchPredicates()) > 0 && !e.watchPending {
		e.watchPending = true
		time.AfterFunc(watchDebounce, e.flushWatchers)
	}
	return nil
}

// supersedingPredicates keep only their latest fact per session: current_url
// is where a session IS, and an older URL is not a second answer.
var supersedingPredicates = map[string]bool{"current_url": true}

// supersedeLocked drops buffered current_url facts replaced by this batch, and
// earlier ones within the batch. It returns the batch to append.
func (e *Engine) supersedeLocked(batch []Fact) []Fact {
	latest := make(map[string]int)
	for i, f := range batch {
		if supersedingPredicates[f.Predicate] && len(f.Args) > 0 {
			latest[f.Predicate+"\x00"+argString(f, 0)] = i
		}
	}
	if len(latest) == 0 {
		return batch
	}
	kept := batch[:0:0]
	for i, f := range batch {
		if supersedingPredicates[f.Predicate] && len(f.Args) > 0 && latest[f.Predicate+"\x00"+argString(f, 0)] != i {
			e.counters.superseded++
			continue
		}
		kept = append(kept, f)
	}
	removed := false
	for predicate := range supersedingPredicates {
		for _, idx := range e.index[predicate] {
			f := e.facts[idx]
			if _, replaced := latest[predicate+"\x00"+argString(f, 0)]; replaced {
				e.facts[idx].Predicate = ""
				removed = true
			}
		}
	}
	if removed {
		compacted := e.facts[:0]
		for _, f := range e.facts {
			if f.Predicate == "" {
				e.counters.superseded++
				continue
			}
			compacted = append(compacted, f)
		}
		e.facts = compacted
		e.rebuildIndex()
		e.baseFresh = false
	}
	return kept
}

// trimLocked evicts down to the trim target by signal class.
func (e *Engine) trimLocked(limit int) {
	classes := classifyFacts(e.facts)
	drop, dropped := evictionPlan(e.facts, classes, trimTarget(limit))
	kept := make([]Fact, 0, trimTarget(limit)+limit/10)
	for i, f := range e.facts {
		if !drop[i] {
			kept = append(kept, f)
		}
	}
	e.facts = kept
	for class := signalLow; class < signalClassCount; class++ {
		e.counters.evicted[class] += int64(dropped[class])
	}
	e.counters.trims++
	e.rebuildIndex()
	e.baseFresh = false
}

// flushWatchers evaluates once for a burst of batches and notifies watchers.
func (e *Engine) flushWatchers() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.watchPending = false
	e.checkAndNotifyWatchersLocked()
}

// checkAndNotifyWatchersLocked evaluates watched predicates and notifies
// subscribers of facts they have not seen.
func (e *Engine) checkAndNotifyWatchersLocked() {
	watchedPredicates := e.WatchPredicates()
	if len(watchedPredicates) == 0 || !e.schemaLoaded {
		return
	}
	for _, predicate := range watchedPredicates {
		derivedFacts, err := e.predicateFactsLocked(context.Background(), predicate)
		if err != nil {
			debugf("watch %s: %v", predicate, err)
			continue
		}
		if fresh := e.filterNewWatchFacts(predicate, derivedFacts); len(fresh) > 0 {
			e.notifySubscribers(predicate, fresh)
		}
	}
}

// predicateFactsLocked returns every current fact of predicate, base or derived.
func (e *Engine) predicateFactsLocked(ctx context.Context, predicate string) ([]Fact, error) {
	store, err := e.readStoreLocked(ctx, predicate)
	if err != nil {
		return nil, err
	}
	arity := e.predicateArityLocked(predicate)
	query := ast.Atom{Predicate: ast.PredicateSym{Symbol: predicate, Arity: arity}}
	if arity >= 0 {
		query.Args = make([]ast.BaseTerm, arity)
		for i := range query.Args {
			query.Args[i] = ast.Variable{Symbol: fmt.Sprintf("W%d", i)}
		}
	}
	var facts []Fact
	err = factstore.NewTemporalFactStoreAdapter(store).GetFacts(query, func(atom ast.Atom) error {
		facts = append(facts, e.atomToFact(atom))
		return nil
	})
	return facts, err
}

func (e *Engine) predicateArityLocked(predicate string) int {
	if e.programInfo != nil {
		for sym := range e.programInfo.Decls {
			if sym.Symbol == predicate {
				return sym.Arity
			}
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

// updateSamplingRate adjusts sampling based on buffer pressure.
// When the buffer is over half full, low-value facts start to be sampled.
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
	if !e.lowValuePredicates[f.Predicate] {
		return true
	}
	if e.samplingRate >= 1.0 {
		return true
	}
	return rand.Float64() < e.samplingRate
}

// SamplingRate returns the current adaptive sampling rate (for diagnostics).
func (e *Engine) SamplingRate() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.samplingRate
}

// Subscribe registers a channel to receive events when a predicate derives new facts.
// Returns a subscription ID for later unsubscription. Facts that already
// exist at subscription time are never reported.
func (e *Engine) Subscribe(predicate string, ch chan WatchEvent) string {
	e.mu.Lock()
	baseline := e.currentPredicateFingerprintsLocked(predicate)
	e.subMu.Lock()
	if len(e.subscriptions[predicate]) == 0 {
		e.watchSeen[predicate] = baseline
	}
	e.subscriptions[predicate] = append(e.subscriptions[predicate], ch)
	e.subMu.Unlock()
	e.mu.Unlock()
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

func (e *Engine) currentPredicateFingerprintsLocked(predicate string) map[string]struct{} {
	seen := make(map[string]struct{})
	if !e.schemaLoaded {
		return seen
	}
	facts, err := e.predicateFactsLocked(context.Background(), predicate)
	if err != nil {
		return seen
	}
	for _, fact := range facts {
		seen[factFingerprint(fact)] = struct{}{}
	}
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

// Query executes a Mangle query and returns every satisfying binding.
//
// Three forms are accepted: a single atom (`net_response(S, R, 500, L, D).`),
// a conjunction (`net_request(S, R, M, U, I, T), net_response(S, R, St, L, D).`)
// which returns the bindings of every named variable, and a clause with a
// body (`q(U) :- net_request(S, R, _, U, _, _), net_response(S, R, 500, _, _).`)
// which returns the head's variables. Every predicate is checked against the
// program before evaluation: an unknown name or a wrong argument count is an
// error naming the declared signature, never an empty result.
func (e *Engine) Query(ctx context.Context, queryStr string) ([]QueryResult, error) {
	if !e.cfg.Enable {
		return nil, errNotReady
	}
	e.mu.RLock()
	ready := e.schemaLoaded
	e.mu.RUnlock()
	if !ready {
		return nil, errNotReady
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
	if strings.HasPrefix(queryStr, "?") {
		queryStr = strings.TrimSpace(strings.TrimPrefix(queryStr, "?"))
	}
	if !strings.HasSuffix(queryStr, ".") {
		queryStr += "."
	}

	clause, err := parseQueryClause(queryStr)
	if err != nil {
		return nil, err
	}
	if err := e.validateSourceComplexity(parse.SourceUnit{Clauses: []ast.Clause{clause}}, 1, e.cfg.GetMaxPremises()); err != nil {
		return nil, fmt.Errorf("query complexity: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkQueryClauseLocked(clause); err != nil {
		return nil, err
	}

	// A query with premises projects its body onto the head's variables and is
	// evaluated through Mangle, so comparisons and negation in the body hold.
	if len(clause.Premises) > 0 {
		return e.queryWithBodyLocked(ctx, clause)
	}

	queryAtom := normalizeAnonymousVariables(clause.Head)
	store, err := e.readStoreLocked(ctx, queryAtom.Predicate.Symbol)
	if err != nil {
		return nil, err
	}

	results := make([]QueryResult, 0)
	err = factstore.NewTemporalFactStoreAdapter(store).GetFacts(queryAtom, func(atom ast.Atom) error {
		if len(results) >= e.cfg.GetMaxQueryResults() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result := make(QueryResult)
		for i, arg := range queryAtom.Args {
			if i >= len(atom.Args) {
				return nil
			}
			val := e.convertConstant(atom.Args[i])
			if varArg, ok := arg.(ast.Variable); ok {
				if existing, exists := result[varArg.Symbol]; exists && !valuesEquivalent(existing, val) {
					return nil
				}
				result[varArg.Symbol] = val
				continue
			}
			if constArg, ok := arg.(ast.Constant); ok && !valuesEquivalent(e.convertConstant(constArg), val) {
				return nil
			}
		}
		results = append(results, result)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("query execution: %w", err)
	}
	return results, nil
}

// queryResultPredicate is the synthetic head predicate used to project the
// results of a conditional query. It is namespaced to avoid colliding with
// user or schema predicates.
const queryResultPredicate = "__browsernerd_query_result"

// queryWithBodyLocked evaluates a conditional query by projecting its body onto
// a synthetic head predicate and evaluating schema + rules + that projection
// into a store of its own. It never changes the live program or derivations,
// and it is bounded like a submitted rule: the evaluation timeout, the created
// fact limit, and max_query_results. e.mu must be held.
func (e *Engine) queryWithBodyLocked(ctx context.Context, clause ast.Clause) ([]QueryResult, error) {
	if !e.schemaLoaded || e.programInfo == nil {
		return nil, errNotReady
	}

	headArgs := clause.Head.Args
	synthSym := ast.PredicateSym{Symbol: queryResultPredicate, Arity: len(headArgs)}
	synthClause := ast.Clause{
		Head:      ast.Atom{Predicate: synthSym, Args: headArgs},
		Premises:  clause.Premises,
		Transform: clause.Transform,
	}

	units := make([]parse.SourceUnit, 0, len(e.schemaUnits)+len(e.ruleUnits)+1)
	units = append(units, e.schemaUnits...)
	units = append(units, e.ruleUnits...)
	units = append(units, parse.SourceUnit{Clauses: []ast.Clause{synthClause}})

	programInfo, err := analysis.Analyze(units, make(map[ast.PredicateSym]ast.Decl))
	if err != nil {
		return nil, fmt.Errorf("analyze query: %w", err)
	}

	e.ensureBaseLocked()
	derived := newIndexedStore()
	overlay := &overlayStore{base: e.base, out: derived}
	program := closureProgram(programInfo, []string{queryResultPredicate})
	if err := e.evalProgramWithTimeout(ctx, program, factstore.NewTemporalFactStoreAdapter(overlay), overlay, "query_body"); err != nil {
		e.baseFresh = false
		return nil, fmt.Errorf("evaluate query: %w", err)
	}

	wildcards := make([]ast.BaseTerm, len(headArgs))
	for i := range wildcards {
		wildcards[i] = ast.Variable{Symbol: fmt.Sprintf("Q%d", i)}
	}

	results := make([]QueryResult, 0)
	err = factstore.NewTemporalFactStoreAdapter(derived).GetFacts(ast.Atom{Predicate: synthSym, Args: wildcards}, func(atom ast.Atom) error {
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

// Evaluate returns the current facts of a predicate, deriving them first when
// a rule defines it.
func (e *Engine) Evaluate(ctx context.Context, predicate string) ([]Fact, error) {
	if !e.cfg.Enable {
		return nil, errNotReady
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.schemaLoaded {
		return nil, errNotReady
	}
	if err := e.checkPredicateNameLocked(predicate); err != nil {
		return nil, err
	}
	facts, err := e.predicateFactsLocked(ctx, predicate)
	if err != nil {
		return nil, fmt.Errorf("eval program: %w", err)
	}
	if facts == nil {
		facts = make([]Fact, 0)
	}
	return facts, nil
}

// QueryTemporal returns the facts of predicate whose time falls in
// [after, before]; a zero bound is open.
func (e *Engine) QueryTemporal(predicate string, after, before time.Time) []Fact {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]Fact, 0)
	if !e.schemaLoaded {
		return results
	}
	arity := e.predicateArityLocked(predicate)
	if arity < 0 {
		return results
	}
	store, err := e.readStoreLocked(context.Background(), predicate)
	if err != nil {
		return results
	}

	args := make([]ast.BaseTerm, arity)
	for i := range args {
		args[i] = ast.Variable{Symbol: fmt.Sprintf("V%d", i)}
	}
	queryAtom := ast.Atom{Predicate: ast.PredicateSym{Symbol: predicate, Arity: arity}, Args: args}

	startBound := ast.NegativeInfinity()
	if !after.IsZero() {
		startBound = ast.NewTimestampBound(after)
	}
	endBound := ast.PositiveInfinity()
	if !before.IsZero() {
		endBound = ast.NewTimestampBound(before)
	}

	_ = store.GetFactsDuring(queryAtom, ast.Interval{Start: startBound, End: endBound}, func(tf factstore.TemporalFact) error {
		fact := e.atomToFact(tf.Atom)
		if tf.Interval.Start.Type == ast.TimestampBound {
			fact.Timestamp = tf.Interval.Start.Time()
		}
		results = append(results, fact)
		return nil
	})
	return results
}

// FactsByPredicate returns the buffered facts of predicate.
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

// Ready reports whether the engine has a usable query context.
func (e *Engine) Ready() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.schemaLoaded || !e.cfg.Enable
}

// factToAtom converts a Fact to a Mangle atom.
func (e *Engine) factToAtom(f Fact) ast.Atom {
	args := make([]ast.BaseTerm, len(f.Args))
	for i, arg := range f.Args {
		args[i] = e.toConstant(arg)
	}
	return ast.Atom{
		Predicate: ast.PredicateSym{Symbol: f.Predicate, Arity: len(f.Args)},
		Args:      args,
	}
}

// atomToFact converts a Mangle atom to a Fact stamped now.
func (e *Engine) atomToFact(atom ast.Atom) Fact {
	args := make([]interface{}, len(atom.Args))
	for i, arg := range atom.Args {
		args[i] = e.convertConstant(arg)
	}
	return Fact{
		Predicate: atom.Predicate.Symbol,
		Args:      args,
		Timestamp: time.Now(),
	}
}

// toConstant converts a Go value to a Mangle constant.
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

// convertConstant converts a Mangle constant to a Go value.
func (e *Engine) convertConstant(c ast.BaseTerm) interface{} {
	if c == nil {
		return nil
	}

	if fn, ok := interface{}(c).(func() (string, error)); ok {
		val, err := fn()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return val
	}

	switch term := c.(type) {
	case ast.Constant:
		if term.Type == ast.StringType {
			val, _ := term.StringValue()
			return val
		} else if term.Type == ast.NumberType {
			// NumberValue is a method; returning it uncalled would leak a
			// func into query bindings and break JSON marshaling.
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

// rebuildIndex recomputes the predicate index after the buffer changed shape.
func (e *Engine) rebuildIndex() {
	e.index = make(map[string][]int)
	for i, f := range e.facts {
		e.index[f.Predicate] = append(e.index[f.Predicate], i)
	}
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
