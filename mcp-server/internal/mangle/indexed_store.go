package mangle

import (
	"sort"
	"time"

	"codeberg.org/TauCeti/mangle-go/ast"
	"codeberg.org/TauCeti/mangle-go/factstore"
)

// indexedStore is a temporal fact store that indexes each predicate by its
// first argument, its second argument, and the pair. The factstore
// TemporalStore scans every fact of a predicate on each lookup, so a rule
// joining net_request to net_response on (SessionId, ReqId) costs
// requests x responses; here the join probe is a map lookup, and evaluation
// stays linear in the buffer.
type indexedStore struct {
	preds map[ast.PredicateSym]*predicateTable
	count int
}

type storeEntry struct {
	atom ast.Atom
	tree *factstore.IntervalTree
}

type predicateTable struct {
	entries  map[uint64]*storeEntry
	byFirst  map[uint64][]*storeEntry
	bySecond map[uint64][]*storeEntry
	byPair   map[uint64][]*storeEntry
}

var _ factstore.TemporalFactStore = (*indexedStore)(nil)

func newIndexedStore() *indexedStore {
	return &indexedStore{preds: make(map[ast.PredicateSym]*predicateTable)}
}

func pairHash(a, b uint64) uint64 {
	return a*1099511628211 ^ b
}

func constantHash(term ast.BaseTerm) (uint64, bool) {
	c, ok := term.(ast.Constant)
	if !ok {
		return 0, false
	}
	return c.Hash(), true
}

// candidates returns the entries a query can match, using the narrowest index
// its constant arguments allow. Callers still filter with factstore.Matches.
func (t *predicateTable) candidates(query ast.Atom, fn func(*storeEntry) error) error {
	var list []*storeEntry
	indexed := false
	if len(query.Args) >= 1 {
		h0, ok0 := constantHash(query.Args[0])
		var h1 uint64
		ok1 := false
		if len(query.Args) >= 2 {
			h1, ok1 = constantHash(query.Args[1])
		}
		switch {
		case ok0 && ok1:
			list, indexed = t.byPair[pairHash(h0, h1)], true
		case ok0:
			list, indexed = t.byFirst[h0], true
		case ok1:
			list, indexed = t.bySecond[h1], true
		}
	}
	if indexed {
		for _, entry := range list {
			if err := fn(entry); err != nil {
				return err
			}
		}
		return nil
	}
	for _, entry := range t.entries {
		if err := fn(entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *indexedStore) table(pred ast.PredicateSym) *predicateTable {
	t, ok := s.preds[pred]
	if !ok {
		t = &predicateTable{
			entries:  make(map[uint64]*storeEntry),
			byFirst:  make(map[uint64][]*storeEntry),
			bySecond: make(map[uint64][]*storeEntry),
			byPair:   make(map[uint64][]*storeEntry),
		}
		s.preds[pred] = t
	}
	return t
}

// Add stores atom as valid during interval.
func (s *indexedStore) Add(atom ast.Atom, interval ast.Interval) (bool, error) {
	t := s.table(atom.Predicate)
	hash := atom.Hash()
	entry, ok := t.entries[hash]
	if !ok {
		entry = &storeEntry{atom: atom, tree: factstore.NewIntervalTree()}
		t.entries[hash] = entry
		if len(atom.Args) >= 1 {
			if h0, ok0 := constantHash(atom.Args[0]); ok0 {
				t.byFirst[h0] = append(t.byFirst[h0], entry)
				if len(atom.Args) >= 2 {
					if h1, ok1 := constantHash(atom.Args[1]); ok1 {
						t.byPair[pairHash(h0, h1)] = append(t.byPair[pairHash(h0, h1)], entry)
					}
				}
			}
		}
		if len(atom.Args) >= 2 {
			if h1, ok1 := constantHash(atom.Args[1]); ok1 {
				t.bySecond[h1] = append(t.bySecond[h1], entry)
			}
		}
	}
	if !entry.tree.Insert(interval) {
		return false, nil
	}
	s.count++
	return true, nil
}

// AddEternal stores atom as valid for all time.
func (s *indexedStore) AddEternal(atom ast.Atom) (bool, error) {
	return s.Add(atom, ast.EternalInterval())
}

func (s *indexedStore) each(query ast.Atom, fn func(*storeEntry) error) error {
	t, ok := s.preds[query.Predicate]
	if !ok {
		return nil
	}
	return t.candidates(query, func(entry *storeEntry) error {
		if !factstore.Matches(query.Args, entry.atom.Args) {
			return nil
		}
		return fn(entry)
	})
}

// GetFactsAt returns facts matching query that are valid at t.
func (s *indexedStore) GetFactsAt(query ast.Atom, t time.Time, fn func(factstore.TemporalFact) error) error {
	timestamp := t.UnixNano()
	return s.each(query, func(entry *storeEntry) error {
		return entry.tree.QueryPoint(timestamp, func(interval ast.Interval) error {
			return fn(factstore.TemporalFact{Atom: entry.atom, Interval: interval})
		})
	})
}

// GetFactsDuring returns facts matching query that overlap interval.
func (s *indexedStore) GetFactsDuring(query ast.Atom, interval ast.Interval, fn func(factstore.TemporalFact) error) error {
	start := factstore.GetStartTime(interval)
	end := factstore.GetEndTime(interval)
	return s.each(query, func(entry *storeEntry) error {
		return entry.tree.QueryRange(start, end, func(factInterval ast.Interval) error {
			return fn(factstore.TemporalFact{Atom: entry.atom, Interval: factInterval})
		})
	})
}

// GetAllFacts returns every interval of every fact matching query. An empty
// predicate symbol matches every fact.
func (s *indexedStore) GetAllFacts(query ast.Atom, fn func(factstore.TemporalFact) error) error {
	visit := func(entry *storeEntry) error {
		return entry.tree.All(func(interval ast.Interval) error {
			return fn(factstore.TemporalFact{Atom: entry.atom, Interval: interval})
		})
	}
	if query.Predicate.Symbol == "" {
		for _, t := range s.preds {
			for _, entry := range t.entries {
				if err := visit(entry); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return s.each(query, visit)
}

// ContainsAt reports whether atom is valid at t.
func (s *indexedStore) ContainsAt(atom ast.Atom, t time.Time) bool {
	table, ok := s.preds[atom.Predicate]
	if !ok {
		return false
	}
	entry, ok := table.entries[atom.Hash()]
	if !ok {
		return false
	}
	found := false
	_ = entry.tree.QueryPoint(t.UnixNano(), func(ast.Interval) error {
		found = true
		return nil
	})
	return found
}

// ListPredicates returns every predicate with at least one fact.
func (s *indexedStore) ListPredicates() []ast.PredicateSym {
	out := make([]ast.PredicateSym, 0, len(s.preds))
	for pred := range s.preds {
		out = append(out, pred)
	}
	return out
}

// EstimateFactCount returns the number of (atom, interval) pairs.
func (s *indexedStore) EstimateFactCount() int {
	return s.count
}

// Coalesce merges overlapping or adjacent intervals of each fact of predicate.
func (s *indexedStore) Coalesce(predicate ast.PredicateSym) error {
	t, ok := s.preds[predicate]
	if !ok {
		return nil
	}
	for _, entry := range t.entries {
		if entry.tree.Size() <= 1 {
			continue
		}
		var intervals []ast.Interval
		_ = entry.tree.All(func(interval ast.Interval) error {
			intervals = append(intervals, interval)
			return nil
		})
		merged := coalesceIntervals(intervals)
		s.count -= len(intervals) - len(merged)
		entry.tree.Rebuild(merged)
	}
	return nil
}

// Merge adds every fact of other.
func (s *indexedStore) Merge(other factstore.ReadOnlyTemporalFactStore) error {
	for _, pred := range other.ListPredicates() {
		if err := other.GetAllFacts(ast.NewQuery(pred), func(tf factstore.TemporalFact) error {
			_, err := s.Add(tf.Atom, tf.Interval)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// overlayStore reads the base facts and one evaluation's derivations as one
// store, and writes only to the derivations. The base store is shared by
// every evaluation, so an evaluation never copies it and never changes it.
type overlayStore struct {
	base *indexedStore
	out  *indexedStore
}

var _ factstore.TemporalFactStore = (*overlayStore)(nil)

func (s *overlayStore) GetFactsAt(query ast.Atom, t time.Time, fn func(factstore.TemporalFact) error) error {
	if err := s.base.GetFactsAt(query, t, fn); err != nil {
		return err
	}
	return s.out.GetFactsAt(query, t, fn)
}

func (s *overlayStore) GetFactsDuring(query ast.Atom, interval ast.Interval, fn func(factstore.TemporalFact) error) error {
	if err := s.base.GetFactsDuring(query, interval, fn); err != nil {
		return err
	}
	return s.out.GetFactsDuring(query, interval, fn)
}

func (s *overlayStore) GetAllFacts(query ast.Atom, fn func(factstore.TemporalFact) error) error {
	if err := s.base.GetAllFacts(query, fn); err != nil {
		return err
	}
	return s.out.GetAllFacts(query, fn)
}

func (s *overlayStore) ContainsAt(atom ast.Atom, t time.Time) bool {
	return s.base.ContainsAt(atom, t) || s.out.ContainsAt(atom, t)
}

func (s *overlayStore) ListPredicates() []ast.PredicateSym {
	seen := make(map[ast.PredicateSym]bool)
	out := make([]ast.PredicateSym, 0, len(s.base.preds)+len(s.out.preds))
	for _, store := range []*indexedStore{s.base, s.out} {
		for pred := range store.preds {
			if !seen[pred] {
				seen[pred] = true
				out = append(out, pred)
			}
		}
	}
	return out
}

func (s *overlayStore) EstimateFactCount() int {
	return s.base.count + s.out.count
}

func (s *overlayStore) Add(atom ast.Atom, interval ast.Interval) (bool, error) {
	return s.out.Add(atom, interval)
}

func (s *overlayStore) AddEternal(atom ast.Atom) (bool, error) {
	return s.out.AddEternal(atom)
}

func (s *overlayStore) Coalesce(predicate ast.PredicateSym) error {
	return s.out.Coalesce(predicate)
}

func (s *overlayStore) Merge(other factstore.ReadOnlyTemporalFactStore) error {
	return s.out.Merge(other)
}

// coalesceIntervals merges overlapping or adjacent concrete intervals and
// keeps unbounded ones as they are.
func coalesceIntervals(intervals []ast.Interval) []ast.Interval {
	if len(intervals) <= 1 {
		return intervals
	}
	var concrete, other []ast.Interval
	for _, i := range intervals {
		if i.Start.Type == ast.TimestampBound && i.End.Type == ast.TimestampBound {
			concrete = append(concrete, i)
		} else {
			other = append(other, i)
		}
	}
	if len(concrete) <= 1 {
		return append(concrete, other...)
	}
	sort.Slice(concrete, func(i, j int) bool { return concrete[i].Start.Timestamp < concrete[j].Start.Timestamp })
	result := []ast.Interval{concrete[0]}
	for _, curr := range concrete[1:] {
		last := &result[len(result)-1]
		if last.End.Timestamp >= curr.Start.Timestamp-1 {
			if curr.End.Timestamp > last.End.Timestamp {
				last.End = curr.End
			}
			continue
		}
		result = append(result, curr)
	}
	return append(result, other...)
}
