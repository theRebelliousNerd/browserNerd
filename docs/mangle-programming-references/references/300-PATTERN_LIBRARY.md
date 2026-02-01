# Complete Mangle Examples

## Example 1: Family Tree Analysis

```mangle
# ========================================
# EXTENDED FAMILY RELATIONSHIPS
# ========================================

# Base facts: Parent relationships
parent(/tereus, /itys).
parent(/procne, /itys).
parent(/aedon, /itylus).
parent(/zeus, /artemis).
parent(/leto, /artemis).

# Gender facts
male(/tereus).
male(/itys).
male(/itylus).
male(/zeus).
female(/procne).
female(/aedon).
female(/leto).
female(/artemis).

# Derived relationships
father(F, C) :- parent(F, C), male(F).
mother(M, C) :- parent(M, C), female(M).

sibling(X, Y) :- 
    parent(P, X), 
    parent(P, Y), 
    X != Y.

brother(B, S) :- sibling(B, S), male(B).
sister(S, B) :- sibling(S, B), female(S).

# Grandparent (demonstrates recursion)
grandparent(GP, GC) :- 
    parent(GP, P), 
    parent(P, GC).

# Queries:
# ?father(F, /itys)           → F = /tereus
# ?sibling(/itys, S)          → S = (none)
# ?grandparent(GP, GC)        → GP = /zeus, GC = /itys (if extended)
```

## Example 2: Software Dependency Vulnerability Analysis

```mangle
# ========================================
# COMPREHENSIVE SBOM SECURITY ANALYZER
# ========================================

# === PROJECT INVENTORY ===
project(/web_app, "WebApp", "1.0.0").
project(/api_service, "APIService", "2.1.0").
project(/data_processor, "DataProcessor", "1.5.0").

# === DIRECT DEPENDENCIES ===
depends_on(/web_app, /react, "18.2.0").
depends_on(/web_app, /axios, "1.4.0").
depends_on(/api_service, /express, "4.18.0").
depends_on(/api_service, /log4j, "2.14.0").
depends_on(/data_processor, /api_service, "2.1.0").

# === VULNERABILITY DATABASE ===
cve_affects("log4j", "2.14.0", "CVE-2021-44228", /critical).
cve_affects("axios", "1.4.0", "CVE-2023-45857", /moderate).

# === LICENSE COMPLIANCE ===
library_license(/react, "MIT").
library_license(/axios, "MIT").
library_license(/express, "MIT").
library_license(/log4j, "Apache-2.0").

acceptable_license("MIT").
acceptable_license("Apache-2.0").
acceptable_license("BSD-3-Clause").

# === ANALYSIS RULES ===

# Transitive dependencies
has_dependency(Project, Lib, Version) :- 
    depends_on(Project, Lib, Version).

has_dependency(Project, Lib, Version) :- 
    depends_on(Project, Intermediate, _),
    has_dependency(Intermediate, Lib, Version).

# Vulnerability detection
vulnerable_project(Project, CVE, Severity) :- 
    project(Project, _, _),
    has_dependency(Project, Lib, Version),
    cve_affects(Lib, Version, CVE, Severity).

# License compliance
license_violation(Project, Lib) :- 
    has_dependency(Project, Lib, _),
    library_license(Lib, License),
    not acceptable_license(License).

# Risk categorization
high_risk_project(Project) :- 
    vulnerable_project(Project, _, /critical).

# Aggregated reporting
risk_summary(Project, VulnCount) :- 
    vulnerable_project(Project, _, _) |> 
    do fn:group_by(Project), 
    let VulnCount = fn:Count().

# === QUERIES ===
# ?vulnerable_project(P, CVE, Sev)
# ?high_risk_project(P)
# ?license_violation(P, Lib)
# ?risk_summary(P, Count)
```

## Example 3: Graph Reachability

```mangle
# ========================================
# NETWORK REACHABILITY ANALYSIS
# ========================================

# Network topology
edge(/router1, /router2).
edge(/router2, /router3).
edge(/router3, /router4).
edge(/router1, /router5).
edge(/router5, /router4).

# Bidirectional edges
bidirectional(/server1, /router1).
bidirectional(/server2, /router4).

# Make bidirectional symmetric
edge(X, Y) :- bidirectional(X, Y).
edge(Y, X) :- bidirectional(X, Y).

# Transitive closure (reachability)
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- edge(X, Y), reachable(Y, Z).

# Path length calculation
path_length(X, Y, 1) :- edge(X, Y).
path_length(X, Z, Len) :- 
    edge(X, Y), 
    path_length(Y, Z, SubLen) |> 
    let Len = fn:plus(SubLen, 1).

# Shortest path
shortest_path(X, Y, MinLen) :- 
    path_length(X, Y, Len) |> 
    do fn:group_by(X, Y), 
    let MinLen = fn:Min(Len).

# === QUERIES ===
# ?reachable(/server1, /server2)
# ?shortest_path(/server1, /server2, Len)
```

## Example 4: Infrastructure Policy Compliance

```mangle
# ========================================
# CLOUD INFRASTRUCTURE POLICY CHECKER
# ========================================

# === INFRASTRUCTURE STATE ===
server(/web_01, /us_east, /production, /running).
server(/db_01, /us_east, /production, /running).
server(/cache_01, /eu_west, /staging, /running).
server(/analytics_01, /ap_south, /production, /stopped).

# Data classification
contains_pii(/db_01, true).
contains_pii(/web_01, false).
contains_pii(/cache_01, true).
contains_pii(/analytics_01, true).

# === POLICY RULES ===

# PII data must be in approved regions
approved_pii_region(/us_east).
approved_pii_region(/us_west).

pii_violation(Server, Region) :- 
    server(Server, Region, _, _),
    contains_pii(Server, true),
    not approved_pii_region(Region).

# Production servers must be running
availability_violation(Server) :- 
    server(Server, _, /production, /stopped).

# Production servers should be redundant
redundant(Server) :- 
    server(Server, Region1, /production, _),
    server(OtherServer, Region2, /production, _),
    Server != OtherServer,
    Region1 != Region2.

single_point_of_failure(Server) :- 
    server(Server, _, /production, _),
    not redundant(Server).

# Compliance summary
compliance_issues(Type, Count) :- 
    pii_violation(_, _) |> 
    do fn:group_by(), 
    let Count = fn:Count() |> 
    let Type = /pii_violation.

compliance_issues(Type, Count) :- 
    availability_violation(_) |> 
    do fn:group_by(), 
    let Count = fn:Count() |> 
    let Type = /availability_violation.

# === QUERIES ===
# ?pii_violation(S, R)
# ?single_point_of_failure(S)
# ?compliance_issues(Type, Count)
```

## Example 5: Volunteer Skills Matching

```mangle
# ========================================
# VOLUNTEER-PROJECT MATCHER
# ========================================

# Volunteers and their skills
volunteer(1, "Alice", /teaching).
volunteer(2, "Bob", /software).
volunteer(3, "Carol", /teaching).
volunteer(4, "David", /software).
volunteer(5, "Eve", /design).

# Projects and required skills
project(/literacy, /teaching, 5).  # needs 5 hours/week
project(/website, /software, 10).
project(/app, /software, 20).
project(/branding, /design, 8).

# Volunteer availability
available_hours(1, 6).
available_hours(2, 15).
available_hours(3, 4).
available_hours(4, 25).
available_hours(5, 10).

# === MATCHING RULES ===

# Can help (has skill)
can_help(Volunteer, Name, Project) :- 
    volunteer(Volunteer, Name, Skill),
    project(Project, Skill, _).

# Can commit (has time)
can_commit(Volunteer, Project) :- 
    volunteer(Volunteer, _, _),
    project(Project, _, RequiredHours),
    available_hours(Volunteer, AvailableHours),
    AvailableHours >= RequiredHours.

# Good match (skill + time)
good_match(Volunteer, Name, Project) :- 
    can_help(Volunteer, Name, Project),
    can_commit(Volunteer, Project).

# Count volunteers per project
volunteers_per_project(Project, Count) :- 
    can_help(_, _, Project) |> 
    do fn:group_by(Project), 
    let Count = fn:Count().

# Understaffed projects
needs_volunteers(Project) :- 
    project(Project, _, _),
    volunteers_per_project(Project, Count),
    Count < 2.

needs_volunteers(Project) :- 
    project(Project, _, _),
    not volunteers_per_project(Project, _).

# === QUERIES ===
# ?good_match(V, Name, P)
# ?needs_volunteers(P)
```

## Example 6: Travel Route Planning

```mangle
# ========================================
# FLIGHT ROUTE OPTIMIZER
# ========================================

# Direct connections (from, to, flight_code, price)
direct_conn(/sfo, /ord, "UA123", 250).
direct_conn(/ord, /jfk, "AA456", 180).
direct_conn(/sfo, /jfk, "DL789", 400).
direct_conn(/jfk, /lhr, "BA001", 600).
direct_conn(/ord, /lhr, "UA002", 650).

# Find routes with up to one connection
route(Start, Dest, Codes, Price) :- 
    direct_conn(Start, Dest, Code, P) |> 
    let Codes = [Code],
    let Price = P.

route(Start, Dest, Codes, Price) :- 
    direct_conn(Start, Mid, C1, P1),
    direct_conn(Mid, Dest, C2, P2) |> 
    let Codes = [C1, C2],
    let Price = fn:plus(P1, P2).

# Find cheapest route
cheapest_route(Start, Dest, MinPrice) :- 
    route(Start, Dest, _, Price) |> 
    do fn:group_by(Start, Dest), 
    let MinPrice = fn:Min(Price).

# === QUERIES ===
# ?route(/sfo, /lhr, Codes, Price)
# ?cheapest_route(/sfo, /lhr, Price)
```

## Example 7: Data Lineage Tracking

```mangle
# ========================================
# DATA LINEAGE ANALYZER
# ========================================

# Source tables
source_table(/customers).
source_table(/orders).
source_table(/products).

# ETL transformations
transforms(/customers, /customer_cleaned).
transforms(/orders, /order_enriched).
transforms(/customer_cleaned, /customer_segmented).
transforms(/order_enriched, /order_segmented).

# Joins
joins(/customer_segmented, /order_segmented, /customer_orders).

# Final outputs
final_table(/customer_orders).
final_table(/customer_segmented).

# === LINEAGE RULES ===

# Direct dependency
depends_on(Target, Source) :- transforms(Source, Target).
depends_on(Target, Source) :- joins(Source, _, Target).
depends_on(Target, Source) :- joins(_, Source, Target).

# Transitive dependency
depends_on(Target, Source) :- 
    depends_on(Target, Intermediate),
    depends_on(Intermediate, Source).

# Source lineage (all sources for a table)
source_lineage(Table, Source) :- 
    final_table(Table),
    depends_on(Table, Source),
    source_table(Source).

# Lineage depth
lineage_depth(Table, Depth) :- 
    source_lineage(Table, _) |> 
    do fn:group_by(Table), 
    let Depth = fn:Count().

# === QUERIES ===
# ?source_lineage(/customer_orders, Source)
# ?lineage_depth(Table, Depth)
```

## Running Examples

### Interactive Interpreter

```bash
# Start interpreter
mg

# Load example
::load family_tree.mg

# Query
?sibling(X, Y)

# Show all predicates
::show all
```

### Go Program

```go
package main

import (
    "fmt"
    "github.com/google/mangle/parse"
    "github.com/google/mangle/analysis"
    "github.com/google/mangle/engine"
    "github.com/google/mangle/factstore"
)

func main() {
    // Load example from file
    source := readFile("vulnerability_analysis.mg")
    
    // Parse and analyze
    sourceUnits, _ := parse.Unit(source)
    programInfo, _ := analysis.AnalyzeOneUnit(sourceUnits)
    
    // Evaluate
    store := factstore.NewSimpleInMemoryStore()
    engine.EvalProgram(programInfo, store)
    
    // Query results
    vulnPred := ast.PredicateSym{Symbol: "vulnerable_project", Arity: 3}
    facts := store.GetFacts(vulnPred)
    
    for _, fact := range facts {
        fmt.Printf("Vulnerable: %v\n", fact)
    }
}
```

---

# Additional Comprehensive Patterns

## Optimization Patterns

### Early Filtering
```mangle
# ❌ BAD: Cartesian product first
bad(X, Y) :- large_table1(X), large_table2(Y), filter(X, Y).

# ✅ GOOD: Filter first
good(X, Y) :- filter(X, Y), large_table1(X), large_table2(Y).
```

### Selectivity Ordering
```mangle
# Place most selective (fewest results) predicates first
efficient(X, Y, Z) :- 
    rare_condition(Y, Z),      # 10 results
    join_condition(X, Y),      # 100 results after join
    common_condition(X).       # Check only 100 items
```

### Materialization
```mangle
# Materialize expensive intermediate computations
expensive_step(X, Y) :- 
    complex_computation(X, Y).

final_result(X, Y, Z) :- 
    expensive_step(X, Y),
    cheap_step(Y, Z).
```

## Domain-Specific Patterns

### Access Control & Security
```mangle
# Permission via direct grant or role
has_permission(User, Resource, Action) :- 
    grant(User, Resource, Action).

has_permission(User, Resource, Action) :- 
    user_role(User, Role),
    role_permission(Role, Resource, Action).

# Effective permissions (after denials)
effective_permission(U, R, A) :- 
    has_permission(U, R, A),
    not deny(U, R, A).
```

### Temporal Reasoning
```mangle
# Valid during time period
valid_at(Fact, Time) :- 
    fact_validity(Fact, Start, End),
    Time >= Start,
    Time <= End.

# Currently valid facts
current_facts(Fact) :- 
    fact_validity(Fact, Start, End),
    current_time(Now),
    Now >= Start,
    Now <= End.
```

### Provenance Tracking
```mangle
# Track source of derived facts
derived(X, Y, Source) :- 
    base_fact(X, Y) |> 
    let Source = [/base, X, Y].

derived(X, Z, Source) :- 
    derived(X, Y, SourceSoFar),
    transformation(Y, Z) |>
    let Source = [/transform | SourceSoFar].
```

## Complete Pattern Index

### By SQL Equivalent

| SQL Pattern | Mangle Equivalent | Section |
|-------------|------------------|---------|
| SELECT DISTINCT | Rule head variables | Example 2 |
| WHERE | Additional body atoms | Example 1 |
| JOIN | Shared variables | Example 2 |
| LEFT JOIN | Rule + negation | Negation Patterns |
| UNION | Multiple rules | Example 1 |
| GROUP BY | fn:group_by() | Aggregation |
| HAVING | Filter after aggregation | Advanced Aggregation |
| WITH RECURSIVE | Recursive rules | Example 3 |
| NOT EXISTS | Negation | Example 4 |

### By Datalog Pattern

| Pattern Name | Mangle Code | Use Case |
|--------------|-------------|----------|
| Transitive Closure | reachable | Graph reachability |
| Same Generation | via common ancestor | Genealogy |
| Connected Components | symmetric + transitive | Network analysis |
| Topological Sort | level assignment | Dependency ordering |
| Bill of Materials | recursive qty multiplication | Manufacturing |
| Data Lineage | recursive dependency tracking | ETL/data governance |

---

**This pattern library provides copy-paste ready solutions for every common scenario. Combine patterns to solve complex problems.**
