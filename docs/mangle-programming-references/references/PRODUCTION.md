# Production Deployment Guide

## Architecture Patterns

### Pattern 1: Embedded Analysis Engine

```
Application Code
    ↓
Mangle Library (embedded)
    ↓
Query Data → Derive Facts → Apply Rules → Return Results
```

**When to use**:
- Analysis needed within application
- Low latency requirements
- Single-machine workload

**Implementation**:
```go
package main

import (
    "github.com/google/mangle/parse"
    "github.com/google/mangle/analysis"
    "github.com/google/mangle/engine"
    "github.com/google/mangle/factstore"
)

func analyzeProject(projectData string) ([]Vulnerability, error) {
    // Parse Mangle program
    source := loadAnalysisRules()
    sourceUnits, _ := parse.Unit(source)
    programInfo, _ := analysis.AnalyzeOneUnit(sourceUnits)
    
    // Create store and load project data
    store := factstore.NewSimpleInMemoryStore()
    loadProjectFacts(store, projectData)
    
    // Evaluate
    engine.EvalProgram(programInfo, store)
    
    // Extract results
    return extractVulnerabilities(store), nil
}
```

### Pattern 2: Analysis Service

```
Multiple Clients → gRPC API → Mangle Service → Persistent Store
```

**When to use**:
- Multiple applications need analysis
- Centralized policy management
- Shared fact database

**Implementation** (based on github.com/burakemir/mangle-service):

```bash
# Start service
go run server/main.go --db=mangle.db.gz --source=analysis.mg

# Query via gRPC
grpcurl -plaintext -proto mangle.proto \
    -d '{"query": "vulnerable_project(X)"}' \
    localhost:8080 mangle.Mangle.Query
```

### Pattern 3: Batch Processing Pipeline

```
Data Sources → ETL → Mangle Analysis → Results → Downstream Systems
```

**When to use**:
- Scheduled analysis jobs
- Data warehouse integration
- Large-scale batch processing

## Monitoring & Observability

### Performance Metrics

```go
// Capture evaluation statistics
stats, err := engine.EvalProgramWithStats(programInfo, store)
if err != nil {
    log.Fatal(err)
}

// Log stratum evaluation times
for i, duration := range stats.Duration {
    log.Printf("Stratum %d took %v", i, duration)
}

// Log total fact counts
log.Printf("Total strata: %d", len(stats.Strata))
```

### Key Metrics to Track

- **Evaluation duration per stratum**
- **Total fact count over time**
- **Rule evaluation frequency**
- **Memory usage patterns**
- **Query response times**

### Alerting Thresholds

```go
const (
    MaxEvaluationTime = 30 * time.Second
    MaxFactCount = 10_000_000
    MaxMemoryUsage = 8 * 1024 * 1024 * 1024 // 8GB
)

if stats.TotalDuration > MaxEvaluationTime {
    alert("Evaluation taking too long")
}
```

## Debugging Strategies

### Issue: Slow Evaluation

**Diagnosis**:
1. Check fact counts: `::show all`
2. Identify expensive strata
3. Profile rule evaluation

**Solutions**:
- Reorder predicates (most selective first)
- Break complex rules into stages
- Add fact limits
- Consider sampling for large datasets

### Issue: Unexpected Results

**Diagnosis**:
1. Query intermediate predicates
2. Check for duplicates
3. Verify stratification

**Solutions**:
```mangle
# Add debug predicates
debug_stage1(X) :- first_condition(X).
debug_stage2(X, Y) :- debug_stage1(X), second_condition(Y).
final_result(X, Y) :- debug_stage2(X, Y), final_filter(X, Y).

# Query each stage
?debug_stage1(X)
?debug_stage2(X, Y)
?final_result(X, Y)
```

### Issue: Memory Growth

**Diagnosis**:
```go
// Monitor memory
var m runtime.MemStats
runtime.ReadMemStats(&m)
log.Printf("Alloc = %v MB", m.Alloc / 1024 / 1024)
```

**Solutions**:
- Set fact limits
- Implement fact expiry for time-series data
- Use sampling for large datasets
- Consider custom fact store with disk backing

## Testing Strategies

### Unit Testing Mangle Programs

```mangle
# Define expected results
expected_sibling(/antigone, /ismene).
expected_sibling(/ismene, /antigone).

# Test assertion
test_siblings_correct :-
    expected_sibling(X, Y),
    sibling(X, Y).

# Check all tests pass (fails if any expected missing)
all_tests_pass :-
    expected_sibling(X, Y),
    not sibling(X, Y),
    false.  # Force failure
```

### Integration Testing

```go
func TestVulnerabilityDetection(t *testing.T) {
    source := loadRules("vulnerability_detection.mg")
    store := factstore.NewSimpleInMemoryStore()
    
    // Load test data
    loadFacts(store, "test_dependencies.facts")
    
    // Evaluate
    programInfo, _ := analysis.AnalyzeOneUnit(source)
    engine.EvalProgram(programInfo, store)
    
    // Assert expected results
    vulnPred := ast.PredicateSym{Symbol: "vulnerable_project", Arity: 3}
    facts := store.GetFacts(vulnPred)
    
    assert.Equal(t, 2, len(facts), "Expected 2 vulnerable projects")
}
```

### Load Testing

```go
func BenchmarkLargeFactbase(b *testing.B) {
    store := factstore.NewSimpleInMemoryStore()
    
    // Generate large fact set
    for i := 0; i < 100000; i++ {
        // Add facts
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.EvalProgram(programInfo, store)
    }
}
```

## Scaling Considerations

### Single-Machine Limits

✅ **Handles well**:
- Millions of facts
- Complex recursive queries
- Real-time analysis (<1s)

⚠️ **Consider alternatives**:
- Billions of facts → Sample/partition
- Distributed execution → Not supported
- Streaming updates → Batch instead

### Optimization Checklist

- [ ] Rule ordering optimized (selective first)
- [ ] Stratification verified
- [ ] Fact limits set appropriately
- [ ] Memory usage monitored
- [ ] Performance metrics tracked
- [ ] Indexes on fact store (if custom)

## Error Handling

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| "Variable X not bound" | Safety violation | Add positive atom binding X |
| "Stratification failed" | Negation cycle | Restructure dependencies |
| "Arity mismatch" | Wrong argument count | Check predicate usage |
| "Type mismatch" | Incompatible types | Verify type declarations |
| "Parse error" | Syntax error | Check periods, commas |

### Error Recovery

```go
// Robust evaluation with recovery
func safeEvaluate(programInfo *analysis.ProgramInfo, store factstore.FactStore) error {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Evaluation panic: %v", r)
        }
    }()
    
    return engine.EvalProgram(programInfo, store, 
        engine.WithCreatedFactLimit(1000000))
}
```

## Deployment Checklist

### Pre-deployment

- [ ] All Mangle programs validated
- [ ] Performance benchmarks met
- [ ] Memory limits configured
- [ ] Monitoring in place
- [ ] Error handling tested
- [ ] Documentation complete

### Deployment

- [ ] Gradual rollout strategy
- [ ] Canary testing
- [ ] Rollback plan ready
- [ ] Alert thresholds configured

### Post-deployment

- [ ] Monitor evaluation times
- [ ] Track fact growth
- [ ] Review error logs
- [ ] Optimize based on usage

## Custom Fact Store

For persistence or external data:

```go
type CustomFactStore struct {
    db *sql.DB
}

func (s *CustomFactStore) Add(atom ast.Atom) error {
    // Store in database
    return s.db.Exec("INSERT INTO facts ...")
}

func (s *CustomFactStore) GetFacts(pred ast.PredicateSym) []ast.Atom {
    // Query from database
    rows, _ := s.db.Query("SELECT * FROM facts WHERE ...")
    return parseRows(rows)
}

// Implement other FactStore methods
```

## Security Considerations

### Input Validation

```go
// Sanitize user input before parsing
func validateInput(input string) error {
    if len(input) > 100000 {
        return errors.New("input too large")
    }
    // Check for suspicious patterns
    return nil
}
```

### Resource Limits

```go
// Prevent resource exhaustion
engine.EvalProgram(programInfo, store,
    engine.WithCreatedFactLimit(1000000),
    engine.WithTimeout(30 * time.Second))
```

### Access Control

```go
// Restrict which predicates can be queried
allowedPredicates := map[string]bool{
    "public_data": true,
    "vulnerability_report": true,
}

if !allowedPredicates[requestedPredicate] {
    return errors.New("access denied")
}
```
