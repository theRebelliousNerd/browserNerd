package mangle

import (
	"sort"
	"strings"
	"time"
)

// Stats is the engine's self-report for diagnostics (the doctor report).
type Stats struct {
	Facts        int            `json:"facts"`
	Limit        int            `json:"limit"`
	TrimTarget   int            `json:"trim_target"`
	FactsByClass map[string]int `json:"facts_by_class"`
	// Evicted counts facts shed by retention, by signal class.
	Evicted      map[string]int64 `json:"evicted"`
	EvictedTotal int64            `json:"evicted_total"`
	Trims        int64            `json:"trims"`
	// Superseded counts current_url facts replaced by a newer one.
	Superseded   int64   `json:"superseded"`
	SamplingRate float64 `json:"sampling_rate"`
	SampledOut   int64   `json:"sampled_out"`
	// Rules counts the program's rules; RuntimeRules the submitted units.
	Rules        int      `json:"rules"`
	RuntimeRules int      `json:"runtime_rules"`
	Predicates   int      `json:"predicates"`
	Modules      []string `json:"modules"`
	Ready        bool     `json:"ready"`
	// CachedDerivations counts derived-predicate evaluations still current
	// (any write clears them).
	CachedDerivations int     `json:"cached_derivations"`
	Evaluations       int64   `json:"evaluations"`
	LastRebuildMs     float64 `json:"last_rebuild_ms"`
	LastEvalMs        float64 `json:"last_eval_ms"`
	LastEvalError     string  `json:"last_eval_error,omitempty"`
}

func millis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// Stats reports buffer occupancy, retention, sampling, and program shape.
func (e *Engine) Stats() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	s := Stats{
		Facts:             len(e.facts),
		Limit:             e.cfg.FactBufferLimit,
		FactsByClass:      make(map[string]int, signalClassCount),
		Evicted:           make(map[string]int64, signalClassCount),
		Trims:             e.counters.trims,
		Superseded:        e.counters.superseded,
		SamplingRate:      e.samplingRate,
		SampledOut:        e.counters.sampledOut,
		RuntimeRules:      len(e.ruleUnits),
		Ready:             e.schemaLoaded || !e.cfg.Enable,
		CachedDerivations: len(e.derivations),
		Evaluations:       e.counters.evaluations,
		LastRebuildMs:     millis(e.counters.lastBuild),
		LastEvalMs:        millis(e.counters.lastEval),
		LastEvalError:     e.counters.lastEvalError,
	}
	if s.Limit > 0 {
		s.TrimTarget = trimTarget(s.Limit)
	}
	for _, class := range classifyFacts(e.facts) {
		s.FactsByClass[class.String()]++
	}
	for class := signalLow; class < signalClassCount; class++ {
		s.Evicted[class.String()] = e.counters.evicted[class]
		s.EvictedTotal += e.counters.evicted[class]
	}
	if e.programInfo != nil {
		s.Rules = len(e.programInfo.Rules)
		for sym, decl := range e.programInfo.Decls {
			if decl != nil && !decl.IsSynthetic() && !strings.HasPrefix(sym.Symbol, "__") {
				s.Predicates++
			}
		}
	}
	for _, module := range e.schemaModules {
		s.Modules = append(s.Modules, module.Label())
	}
	sort.Strings(s.Modules)
	return s
}
