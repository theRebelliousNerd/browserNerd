# 900: Ecosystem - Tools, Libraries, and Integrations

**Purpose**: Production deployment, Go integration, and ecosystem tools.

## Go Integration

### Basic Embedding
```go
import (
    "github.com/google/mangle/parse"
    "github.com/google/mangle/analysis"
    "github.com/google/mangle/engine"
    "github.com/google/mangle/factstore"
)

func analyzeData(mangleSource string) error {
    // Parse
    sourceUnits, _ := parse.Unit(mangleSource)
    programInfo, _ := analysis.AnalyzeOneUnit(sourceUnits)
    
    // Evaluate
    store := factstore.NewSimpleInMemoryStore()
    engine.EvalProgram(programInfo, store)
    
    // Query results
    facts := store.GetFacts(predicateSym)
    return nil
}
```

### Custom Fact Store
```go
type CustomStore struct {
    db *sql.DB
}

func (s *CustomStore) Add(atom ast.Atom) error {
    // Store in database
}

func (s *CustomStore) GetFacts(pred ast.PredicateSym) []ast.Atom {
    // Query from database
}
// Implement remaining FactStore interface
```

## Production Architectures

### Embedded Analysis
```
Application → Mangle Library → Results
```
**Use case**: Single-machine analysis

### Analysis Service
```
Clients → gRPC API → Mangle Service → Database
```
**Use case**: Centralized policy engine

**Reference**: github.com/burakemir/mangle-service

### Batch Processing
```
Data Sources → ETL → Mangle → Results → Downstream
```
**Use case**: Scheduled analysis jobs

## Monitoring & Observability

### Performance Metrics
```go
stats, _ := engine.EvalProgramWithStats(programInfo, store)

// Log per-stratum duration
for i, duration := range stats.Duration {
    metrics.RecordStratumDuration(i, duration)
}

// Alert on slow evaluation
if stats.TotalDuration > threshold {
    alert("Slow Mangle evaluation")
}
```

### Key Metrics
- Evaluation duration per stratum
- Total fact count
- Memory usage
- Query response times

## Testing Strategies

### Unit Tests
```go
func TestVulnerabilityDetection(t *testing.T) {
    source := loadProgram("vuln_detect.mg")
    store := loadTestData("test_deps.facts")
    
    programInfo, _ := analysis.AnalyzeOneUnit(source)
    engine.EvalProgram(programInfo, store)
    
    assert.Equal(t, 2, countVulnerabilities(store))
}
```

### Load Tests
```go
func BenchmarkLargeFactbase(b *testing.B) {
    store := generateLargeFacts(100000)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.EvalProgram(programInfo, store)
    }
}
```

## Debugging Strategies

### Issue: Slow Evaluation
1. Check fact counts: `::show all`
2. Profile stratum durations
3. Reorder rules (selectivity)
4. Add fact limits

### Issue: Unexpected Results
1. Query intermediate predicates
2. Check stratification: `::show <pred>`
3. Verify variable safety

### Issue: Memory Growth
1. Monitor with runtime.ReadMemStats
2. Set fact limits
3. Implement fact expiry
4. Use custom store with disk backing

## Deployment Checklist

### Pre-deployment
- [ ] Programs validated
- [ ] Benchmarks met
- [ ] Memory limits configured
- [ ] Monitoring in place
- [ ] Error handling tested

### Deployment
- [ ] Gradual rollout
- [ ] Canary testing
- [ ] Rollback plan ready

### Post-deployment
- [ ] Monitor evaluation times
- [ ] Track fact growth
- [ ] Review error logs

## Security Considerations

### Input Validation
```go
func validateInput(input string) error {
    if len(input) > MAX_SIZE {
        return errors.New("input too large")
    }
    // Additional checks
}
```

### Resource Limits
```go
engine.EvalProgram(programInfo, store,
    engine.WithCreatedFactLimit(1000000),
    engine.WithTimeout(30 * time.Second))
```

---

**This completes the ecosystem guide. See PRODUCTION.md for detailed deployment patterns.**
