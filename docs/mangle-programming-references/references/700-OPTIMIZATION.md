# 700: Optimization & Performance Engineering

**Purpose**: Maximize performance for large-scale Mangle programs (millions of facts).

## Rule Ordering Strategy

### Most Selective First
```mangle
# ❌ INEFFICIENT
slow(X, Y, Z) :- 
    common(X),           # 10,000 rows
    common(Y),           # 10,000 rows (100M combinations)
    rare_filter(X, Y, Z). # 100 rows

# ✅ EFFICIENT
fast(X, Y, Z) :- 
    rare_filter(X, Y, Z),  # 100 rows
    common(X),             # Check 100 items
    common(Y).             # Check 100 items
```

### Selectivity Analysis
**Rule**: Order atoms by selectivity (fewest results first)

**Estimate selectivity**:
1. Count facts per predicate
2. Consider join selectivity
3. Place most restrictive first

## Semi-Naive Evaluation Internals

**Naive evaluation**:
- O(iterations) × O(rules) × O(|facts|²)
- Recomputes everything every iteration

**Semi-naive evaluation**:
- O(iterations) × O(rules) × O(|Δfacts| × |facts|)
- Only processes NEW facts (Δ)

**Speedup**: 10-100x for recursive queries

## Memory Management

### Fact Limits
```go
// Prevent runaway computation
store := factstore.NewSimpleInMemoryStore()
engine.EvalProgram(programInfo, store, 
    engine.WithCreatedFactLimit(1000000))
```

### Memory Profiling
```go
var m runtime.MemStats
runtime.ReadMemStats(&m)
log.Printf("Alloc = %v MB", m.Alloc / 1024 / 1024)
```

## Profiling & Benchmarking

### Evaluation Statistics
```go
stats, _ := engine.EvalProgramWithStats(programInfo, store)

for i, duration := range stats.Duration {
    log.Printf("Stratum %d: %v", i, duration)
}
log.Printf("Total strata: %d", len(stats.Strata))
```

### Benchmark Template
```go
func BenchmarkProgram(b *testing.B) {
    // Setup
    store := factstore.NewSimpleInMemoryStore()
    loadFacts(store)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.EvalProgram(programInfo, store)
    }
}
```

## Scaling Limits

### Single-Machine Performance
✅ **Handles well**:
- Millions of facts
- Complex recursive queries
- Real-time (<1s) analysis

⚠️ **Consider alternatives**:
- Billions of facts → Sampling/partitioning
- Distributed → Not supported
- Streaming → Batch processing instead

## Performance Checklist

- [ ] Rules ordered by selectivity
- [ ] Stratification minimized
- [ ] Fact limits configured
- [ ] Memory usage monitored
- [ ] Benchmarks established
- [ ] Profiling enabled for slow queries

---

**See also**: PRODUCTION.md for deployment performance tuning.
