# Advanced Mangle Patterns

## Stratified Negation

### Understanding Stratification

Programs with negation must be **stratified**: predicates partitioned into layers where:
- Positive dependencies allowed within/between strata
- Negative dependencies only from higher to lower strata

```mangle
# Stratum 0: Base facts
vulnerable_lib(Lib, Version) :- cve_database(Lib, Version, _).

# Stratum 1: Uses stratum 0
has_vuln(Project) :- 
    depends_on(Project, Lib, Version),
    vulnerable_lib(Lib, Version).

# Stratum 2: Uses negation of stratum 1
clean_project(Project) :- 
    project(Project),
    not has_vuln(Project).
```

### Stratification Rules
- Compute dependency graph
- Identify strongly connected components
- Topologically sort
- Negation edges must go backward only

## Performance Optimization

### Rule Ordering Strategy

Place **most selective predicates first** to minimize intermediate results:

```mangle
# ❌ INEFFICIENT: Creates huge cross product
bad(X, Y, Z) :- 
    large_table1(X),      # 10,000 rows
    large_table2(Y),      # 10,000 rows
    small_filter(X, Y, Z). # 100 rows matching

# ✅ EFFICIENT: Filter early
good(X, Y, Z) :- 
    small_filter(X, Y, Z), # 100 rows
    large_table1(X),       # 100 joins
    large_table2(Y).       # 100 joins
```

### Semi-Naive Evaluation

Mangle uses **semi-naive evaluation** by default:

**Naive** (slow):
- Recomputes ALL facts every iteration
- O(n × r × |facts|²) per iteration

**Semi-Naive** (fast):
- Only processes NEW facts (deltas)
- O(r × |Δfacts| × |facts|) per iteration

**Benefit**: Dramatically faster for recursive queries.

### Memory Management

```go
// Set fact limit to prevent runaway computation
store := factstore.NewSimpleInMemoryStore()
engine.EvalProgram(programInfo, store, 
    engine.WithCreatedFactLimit(1000000))
```

## Advanced Recursive Patterns

### Path Tracking

```mangle
# Track paths through dependency graph
dependency_path(Project, Lib, Path) :-
    contains_jar_directly(Project, Lib, _) |>
    let Path = [Project, Lib].

dependency_path(Project, Lib, Path) :-
    project_depends(Project, Intermediate),
    dependency_path(Intermediate, Lib, SubPath) |>
    let Path = [Project | SubPath].
```

### Cycle Detection

```mangle
# Detect cycles in dependencies
cycle_edge(X, Y) :- 
    depends_on(X, Y),
    reachable(Y, X).

has_cycle(X) :- cycle_edge(X, _).
```

### Distance/Cost Accumulation

```mangle
# Track minimum distance
min_distance(Start, End, Dist) :-
    edge(Start, End, Dist).

min_distance(Start, End, TotalDist) :-
    edge(Start, Mid, Dist1),
    min_distance(Mid, End, Dist2) |>
    let TotalDist = fn:plus(Dist1, Dist2).
```

## Complex Aggregations

### Multi-level Grouping

```mangle
# Group by category, then summarize
category_stats(Category, Count, TotalValue) :- 
    item(Category, Item, Value) |> 
    do fn:group_by(Category), 
    let Count = fn:Count(),
    let TotalValue = fn:Sum(Value).
```

### Conditional Aggregation

```mangle
# Count only items meeting criteria
high_value_count(Category, Count) :- 
    item(Category, Value) |> 
    do fn:filter(fn:gt(Value, 1000)),
    do fn:group_by(Category), 
    let Count = fn:Count().
```

### Nested Aggregation

```mangle
# Average of category totals
overall_average(Avg) :- 
    category_stats(Cat, Total) |> 
    do fn:group_by(), 
    let Avg = fn:Average(Total).
```

## Type System Advanced Features

### Gradual Typing

```mangle
# Optional type declarations
Decl employee(ID.Type<int>, Name.Type<string>).

# Type inference from usage
employee(1, "Alice").
employee(2, "Bob").

# Type checking at runtime
high_id(Name) :- 
    employee(ID, Name),
    ID > 1000.  # Type checker ensures ID is numeric
```

### Structured Type Declarations

```mangle
Decl person(
    ID.Type<int>,
    Info.Type<{/name: string, /age: int}>
).

# Usage
person(1, {/name: "Alice", /age: 30}).
```

### Union Types

```mangle
# Value can be int OR string
Decl flexible(Value.Type<int | string>).

flexible(42).
flexible("text").
```

## Lattice Support (Experimental)

**Concept**: Maintain only maximal elements in partial order.

**Use cases**:
- Interval analysis (min/max bounds)
- Provenance tracking
- Abstract interpretation

```mangle
# Conceptual: Track minimal prices
best_price(Route, Price) :- 
    connection(Route, Price)
    # Lattice ensures only minimum retained
```

## Production Patterns

### Multi-Source Integration

```mangle
# Combine data from database, API, files
all_vulnerabilities(Lib, CVE, Source) :-
    database_vuln(Lib, CVE) |>
    let Source = /database.

all_vulnerabilities(Lib, CVE, Source) :-
    api_vuln(Lib, CVE) |>
    let Source = /api.

all_vulnerabilities(Lib, CVE, Source) :-
    file_vuln(Lib, CVE) |>
    let Source = /file.
```

### Incremental Updates

```go
// Initial evaluation
store := factstore.NewSimpleInMemoryStore()
engine.EvalProgram(programInfo, store)

// Add new facts
newFacts := []ast.Atom{...}
for _, fact := range newFacts {
    store.Add(fact)
}

// Re-evaluate (only new derivations computed)
engine.EvalProgram(programInfo, store)
```

### Monitoring Evaluation

```go
// Track performance
stats, _ := engine.EvalProgramWithStats(programInfo, store)

for i, duration := range stats.Duration {
    log.Printf("Stratum %d: %v", i, duration)
}
```

## Debugging Complex Programs

### Break into Intermediate Predicates

```mangle
# Instead of complex single rule:
# result(X, Y, Z) :- condition1(X), condition2(Y), condition3(Z), filter(X, Y, Z).

# Break into stages:
stage1(X) :- condition1(X).
stage2(X, Y) :- stage1(X), condition2(Y).
result(X, Y, Z) :- stage2(X, Y), condition3(Z), filter(X, Y, Z).

# Query each stage to isolate issues
```

### Add Explicit Constraints

```mangle
# Make implicit constraints explicit
valid_assignment(Person, Task) :-
    person(Person),
    task(Task),
    has_skill(Person, Skill),
    requires_skill(Task, Skill),
    available(Person, Time),
    scheduled(Task, Time).
```

## Common Advanced Pitfalls

### Unbounded Recursion

```mangle
# ❌ DANGEROUS: No termination
infinite(X) :- infinite(X).

# ❌ DANGEROUS: Unconstrained growth
count_up(N) :- count_up(M), N = fn:plus(M, 1).

# ✅ SAFE: Bounded by condition
countdown(N) :- 
    countdown(M), 
    N = fn:plus(M, 1), 
    N < 1000.
```

### Negation with Unbound Variables

```mangle
# ❌ WRONG
unbound(X) :- not exists(X).

# ✅ CORRECT
bound(X) :- 
    domain(X),  # X bound here
    not exists(X).
```

### Excessive Intermediate Results

```mangle
# ❌ INEFFICIENT: Cartesian product
many_results(X, Y) :- 
    all_x(X),
    all_y(Y).

# ✅ EFFICIENT: Only join when needed
few_results(X, Y) :- 
    all_x(X),
    all_y(Y),
    related(X, Y).  # Selective join condition
```
