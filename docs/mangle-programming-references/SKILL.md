---
name: mangle-programming
description: Master Google's Mangle declarative programming language for deductive database programming, constraint-like reasoning, and software analysis. From basic facts to production deployment, graph traversal to vulnerability detection, theoretical foundations to optimization. Complete encyclopedic reference with progressive disclosure architecture.
license: Apache-2.0
---

# Mangle Programming: The Complete Reference

**Google Mangle** is a declarative deductive database language extending Datalog with practical features for modern software analysis, security evaluation, and multi-source data integration. This skill provides comprehensive coverage from first principles to production deployment.

## Quick Start: Your First Program (5 Minutes)

```mangle
# Facts: What we know
parent(/oedipus, /antigone).
parent(/oedipus, /ismene).

# Rules: What we derive (Head :- Body means "Head is true if Body is true")
sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.

# Query: What we want to know
?sibling(/antigone, X)
# Result: X = /ismene
```

**Essential syntax**:
- `/name` - Named constants (atoms)
- `UPPERCASE` - Variables
- `:-` - Rule implication ("if")
- `,` - Conjunction (AND)
- `.` - Statement terminator (REQUIRED)

## When to Use Mangle

✅ **Perfect for**:
- Dependency analysis (transitive closures)
- Vulnerability detection (CVE propagation)
- Graph reachability and path finding
- Multi-source data integration
- Recursive relationship reasoning
- Security policy compliance
- Knowledge graph queries

❌ **Not suitable for**:
- Optimization problems → Use MiniZinc, OR-Tools
- Numerical constraints → Use Z3, SMT solvers
- Distributed/parallel execution → Single-machine only
- Real-time streaming → Batch processing model

## The Reference Library

This skill uses **progressive disclosure**: Start with quick references below, then explore numbered references for depth.

### Navigation System

**000-Series: Orientation**
- [000-ORIENTATION](references/000-ORIENTATION.md) - How to navigate this library
  - Navigation patterns for different learning paths
  - Skill architecture and reference organization
  - From beginner to expert roadmaps

**100-Series: Fundamentals**
- [100-FUNDAMENTALS](references/100-FUNDAMENTALS.md) - Theory, concepts, and mental models
  - Logic programming foundations
  - Deductive database principles
  - Evaluation models (bottom-up, semi-naive)
  - When Mangle vs Prolog vs SQL vs Datalog

**200-Series: Language Reference**
- [200-SYNTAX_REFERENCE](references/200-SYNTAX_REFERENCE.md) - Complete language specification
  - Every data type, operator, built-in function
  - Grammar, lexical rules, declarations
  - Safety constraints and stratification
  - REPL commands and file format

**300-Series: Pattern Catalog**
- [300-PATTERN_LIBRARY](references/300-PATTERN_LIBRARY.md) - Every common pattern
  - Selection, projection, join, union (SQL equivalents)
  - Recursive patterns (transitive closure, ancestors, paths)
  - Negation patterns (set difference, universal quantification)
  - Aggregation patterns (count, sum, avg, conditional)
  - Domain-specific (access control, temporal, provenance)

**400-Series: Recursion Mastery**
- [400-RECURSION_MASTERY](references/400-RECURSION_MASTERY.md) - Deep dive on recursive techniques
  - Linear vs non-linear recursion
  - Path construction and tracking
  - Cycle detection and prevention
  - Distance/cost accumulation
  - Mutual recursion patterns
  - Termination analysis

**500-Series: Aggregation & Transforms**
- [500-AGGREGATION_TRANSFORMS](references/500-AGGREGATION_TRANSFORMS.md) - Complete aggregation guide
  - Transform pipeline architecture
  - Multi-stage aggregation
  - Conditional aggregation
  - Nested aggregation patterns
  - Window functions (simulated)
  - Complex analytics

**600-Series: Type System**
- [600-TYPE_SYSTEM](references/600-TYPE_SYSTEM.md) - Types, lattices, and gradual typing
  - Type declarations and inference
  - Structured types (maps, structs, lists)
  - Union types and generics
  - Lattice operations (experimental)
  - Type safety and runtime checks

**700-Series: Optimization**
- [700-OPTIMIZATION](references/700-OPTIMIZATION.md) - Performance engineering
  - Rule ordering strategies
  - Selectivity analysis
  - Semi-naive evaluation internals
  - Memory management
  - Profiling and benchmarking
  - Scaling limits

**800-Series: Theory**
- [800-THEORY](references/800-THEORY.md) - Mathematical foundations
  - First-order logic foundations
  - Fixed-point semantics
  - Stratification theory
  - Complexity analysis
  - Comparison with Datalog variants
  - Academic papers and research

**900-Series: Ecosystem**
- [900-ECOSYSTEM](references/900-ECOSYSTEM.md) - Tools, libraries, and integrations
  - Go integration patterns
  - Production deployment architectures
  - Testing and debugging
  - Custom fact stores
  - gRPC service integration
  - Monitoring and observability

### Legacy References (Being Migrated)

These will be consolidated into the numbered system:
- [SYNTAX.md](references/SYNTAX.md) - Basic syntax (→ 200-SYNTAX_REFERENCE)
- [EXAMPLES.md](references/EXAMPLES.md) - Working examples (→ 300-PATTERN_LIBRARY)
- [ADVANCED_PATTERNS.md](references/ADVANCED_PATTERNS.md) - Advanced patterns (→ 400, 500, 700)
- [PRODUCTION.md](references/PRODUCTION.md) - Deployment guide (→ 900-ECOSYSTEM)

## Essential Quick Reference

### Data Types
```mangle
/name               # Named constant (atom)
42, -17, 3.14       # Numbers
"text"              # Strings
[1, 2, 3]           # Lists
{/k: v}             # Maps/Structs
```

### Operators
```mangle
:-                  # Rule implication (if)
,                   # Conjunction (AND)
not                 # Negation (requires stratification)
=, !=, <, >, <=, >= # Comparisons
|>                  # Transform pipeline
```

### Built-in Functions
```mangle
fn:Count()          # Count elements
fn:Sum(V), fn:Max(V), fn:Min(V)  # Aggregations
fn:group_by(V1, V2, ...)          # Group by
fn:plus(A, B), fn:minus(A, B)     # Arithmetic
```

### Core Patterns

**Transitive Closure**:
```mangle
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- edge(X, Y), reachable(Y, Z).
```

**Negation (Set Difference)**:
```mangle
safe(X) :- candidate(X), not excluded(X).
```

**Aggregation**:
```mangle
count_by_category(Cat, N) :-
    item(Cat, _) |>
    do fn:group_by(Cat),
    let N = fn:Count().
```

## Learning Paths

### Path 1: Beginner (0-2 hours)
1. Read SKILL.md (this file) - Quick Start
2. Read [000-ORIENTATION](references/000-ORIENTATION.md) - Understand the library
3. Read [100-FUNDAMENTALS](references/100-FUNDAMENTALS.md) - Sections 1-3 only
4. Try examples from [300-PATTERN_LIBRARY](references/300-PATTERN_LIBRARY.md) - Basic patterns
5. Install and run: `GOBIN=~/bin go install github.com/google/mangle/interpreter/mg@latest`

### Path 2: Intermediate (2-8 hours)
1. Complete beginner path
2. Read [200-SYNTAX_REFERENCE](references/200-SYNTAX_REFERENCE.md) - Full syntax
3. Read [100-FUNDAMENTALS](references/100-FUNDAMENTALS.md) - All sections
4. Study [400-RECURSION_MASTERY](references/400-RECURSION_MASTERY.md)
5. Study [500-AGGREGATION_TRANSFORMS](references/500-AGGREGATION_TRANSFORMS.md)
6. Build: Vulnerability scanner or dependency analyzer

### Path 3: Advanced (8-20 hours)
1. Complete intermediate path
2. Read [600-TYPE_SYSTEM](references/600-TYPE_SYSTEM.md)
3. Read [700-OPTIMIZATION](references/700-OPTIMIZATION.md)
4. Read [800-THEORY](references/800-THEORY.md)
5. Read [900-ECOSYSTEM](references/900-ECOSYSTEM.md)
6. Build: Production-grade analysis service

### Path 4: Expert (20+ hours)
1. Complete advanced path
2. Deep dive all 800-THEORY mathematical foundations
3. Contribute to github.com/google/mangle
4. Build custom fact stores and integrations
5. Optimize large-scale (millions of facts) programs

## Common Pitfalls (Avoid These)

**Pitfall 1: Forgetting periods**
```mangle
# ❌ WRONG
parent(/a, /b)

# ✅ CORRECT
parent(/a, /b).
```

**Pitfall 2: Unbound variables in negation**
```mangle
# ❌ WRONG
bad(X) :- not foo(X).  # X not bound first

# ✅ CORRECT
good(X) :- candidate(X), not foo(X).  # X bound by candidate
```

**Pitfall 3: Cartesian products**
```mangle
# ❌ INEFFICIENT (10K × 10K = 100M intermediate results)
slow(X, Y) :- table1(X), table2(Y), filter(X, Y).

# ✅ EFFICIENT (filter first, ~100 results)
fast(X, Y) :- filter(X, Y), table1(X), table2(Y).
```

## Installation & REPL

```bash
# Install Mangle interpreter
GOBIN=~/bin go install github.com/google/mangle/interpreter/mg@latest

# Start REPL
~/bin/mg

# REPL commands
::load file.mg      # Load program
?predicate(X, Y)    # Query
::show all          # Show all predicates
::help              # Help
Ctrl-D              # Exit
```

## Comparison with Alternatives

| Feature | Mangle | Prolog | SQL | Datalog | Z3/SMT | MiniZinc |
|---------|--------|--------|-----|---------|--------|----------|
| **Logic programming** | ✅ Bottom-up | ✅ Top-down | ❌ | ✅ Bottom-up | ✅ | ❌ |
| **Recursion** | ✅ Native | ✅ Native | ⚠️ CTE only | ✅ Native | ⚠️ Limited | ❌ |
| **Aggregation** | ✅ Transforms | ⚠️ Bagof/setof | ✅ GROUP BY | ⚠️ Limited | ❌ | ⚠️ Limited |
| **Negation** | ✅ Stratified | ✅ NAF | ✅ NOT EXISTS | ✅ Stratified | ✅ | ❌ |
| **Optimization** | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| **Type system** | ⚠️ Optional | ❌ Untyped | ✅ Strong | ⚠️ Optional | ✅ | ✅ |
| **Best for** | Graph analysis | AI/logic | CRUD | Knowledge base | Constraints | Scheduling |

## Resources

- **GitHub**: https://github.com/google/mangle
- **Documentation**: https://mangle.readthedocs.io
- **Go Packages**: https://pkg.go.dev/github.com/google/mangle
- **Demo Service**: https://github.com/burakemir/mangle-service

## Support

For comprehensive answers:
1. Check the numbered references (000-900)
2. Search patterns in 300-PATTERN_LIBRARY
3. Review examples in EXAMPLES.md
4. See GitHub issues for known problems

---

**Next step**: Read [000-ORIENTATION](references/000-ORIENTATION.md) to understand how to navigate this encyclopedic reference system.
