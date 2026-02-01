# 100: Fundamentals - Theory, Concepts, and Mental Models

**Purpose**: Build the conceptual foundation for Mangle programming. Understand WHY, not just HOW.

## Quick Reference

- **Section 1**: What is Mangle? (5 min)
- **Section 2**: Logic programming foundations (10 min)
- **Section 3**: Evaluation model (15 min)
- **Section 4**: Mangle vs alternatives (10 min)
- **Section 5**: Mental models for success (10 min)

---

## Section 1: What is Mangle?

### The 30-Second Pitch

Mangle is a **declarative deductive database language** that lets you:
1. State facts (what you know)
2. Write rules (what follows logically)
3. Ask questions (what you want to know)

The system automatically derives all consequences.

### Core Philosophy

**Declarative**: Say WHAT you want, not HOW to compute it
```mangle
# Declarative: "Siblings share a parent"
sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.

# NOT procedural: "Loop through parents, check children, compare..."
```

**Deductive**: Derive new facts from existing facts + rules
```mangle
# Given facts:
parent(/a, /b).
parent(/a, /c).

# And rule:
sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.

# System deduces:
sibling(/b, /c).
sibling(/c, /b).
```

**Database-oriented**: Designed for querying data, not computation
- Think: "Find all X where..." not "Calculate result"
- Optimized for recursive graph queries
- Bottom-up evaluation (explained in Section 3)

### Design Space

Mangle occupies a unique position:

```
Datalog ──extends──> Mangle ──extends──> ?
                      ↓
                  Adds:
                  - Aggregation (fn:Count, fn:Sum)
                  - Transforms (|> pipeline)
                  - Structured types (maps, structs)
                  - Optional types (gradual typing)
```

**Datalog**: Pure logic, no aggregation
**Mangle**: Practical extensions for real-world analysis
**Not Added**: Optimization solvers, constraint propagation

---

## Section 2: Logic Programming Foundations

### First-Order Logic Connection

Mangle rules are **first-order logic implications**:

```
Logic:    ancestor(X, Y) ← parent(X, Y)
Mangle:   ancestor(X, Y) :- parent(X, Y).
```

**Symbols mapping**:
- `←` (logic) = `:-` (Mangle)
- `∧` (and) = `,` (comma)
- `∨` (or) = multiple rules
- `¬` (not) = `not`
- `∀` (forall) = variables in head
- `∃` (exists) = variables in body only

### Horn Clauses

Mangle rules are **Horn clauses**:
```
head :- body1, body2, ..., bodyN.
```

**Restrictions**:
- Exactly ONE positive literal in head
- Zero or more literals in body
- No disjunction in head (but multiple rules OK)

**Why Horn clauses?**
- Decidable inference
- Efficient evaluation (polynomial data complexity)
- Simple semantics

### Herbrand Universe

**Herbrand base**: All ground atoms that can be formed
```mangle
# Given:
person(/alice).
person(/bob).
likes(/alice, /bob).

# Herbrand base includes:
person(/alice).     # ✅ True
person(/bob).       # ✅ True
likes(/alice, /bob). # ✅ True
likes(/bob, /alice). # ❌ False (not derivable)
```

**Closed-world assumption**: If not provable, then false.

### Unification

**Key operation** in logic programming:

```mangle
parent(P, /antigone)  # Pattern with variable P
parent(/oedipus, X)   # Pattern with variable X

# Unify with fact:
parent(/oedipus, /antigone).  

# Bindings: P = /oedipus, X = /antigone
```

**Mangle uses unification** to:
- Match patterns against facts
- Bind variables
- Propagate bindings through rules

---

## Section 3: Evaluation Model

### Bottom-Up vs Top-Down

**Top-Down (Prolog-style)**:
- Start with query goal
- Work backward to facts
- Depth-first search
- Can loop infinitely

**Bottom-Up (Mangle/Datalog)**:
- Start with facts
- Apply rules to derive new facts
- Repeat until fixpoint
- Guaranteed termination (for positive programs)

```mangle
# Example: Reachability
edge(/a, /b).
edge(/b, /c).

reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- reachable(X, Y), edge(Y, Z).

# Bottom-up evaluation:
# Iteration 0: {edge(/a, /b), edge(/b, /c)}
# Iteration 1: {reachable(/a, /b), reachable(/b, /c)}  [from base rule]
# Iteration 2: {reachable(/a, /c)}  [from recursive rule]
# Iteration 3: {} [fixpoint reached]
```

### Naive vs Semi-Naive Evaluation

**Naive** (simple but slow):
```
Iteration 1: Apply all rules to ALL facts
Iteration 2: Apply all rules to ALL facts (including derived)
...
```
**Problem**: Recomputes everything every time.

**Semi-Naive** (Mangle's default):
```
Δ₀ = base facts
Iteration 1: Apply rules using Δ₀, produce Δ₁
Iteration 2: Apply rules using Δ₁ + ALL facts, produce Δ₂
...
```
**Key**: Only NEW facts (Δ) trigger re-evaluation.

**Performance impact**:
- Naive: O(n) iterations × O(|facts|²) work
- Semi-naive: O(n) iterations × O(|Δ| × |facts|) work
- **10-100x faster** for recursive queries

### Stratified Negation

**Problem**: Negation can create circular dependencies.

```mangle
# Dangerous:
p(X) :- not q(X).
q(X) :- not p(X).
# What does this mean? Paradox!
```

**Solution**: Stratification

1. **Compute dependency graph**:
   - Positive edge: `p` uses `q` positively
   - Negative edge: `p` uses `not q`

2. **Partition into strata**:
   - Negative edges must go BACKWARD only
   - Topologically sort

3. **Evaluate stratum-by-stratum**:
   - Stratum 0 → fixpoint
   - Stratum 1 (using 0's results) → fixpoint
   - ...

**Example**:
```mangle
# Stratum 0
base(X) :- source(X).

# Stratum 1 (uses stratum 0 positively)
derived(X) :- base(X), condition(X).

# Stratum 2 (uses stratum 1 negatively)
final(X) :- base(X), not derived(X).
```

**Evaluation order**: 0 → 1 → 2 (guaranteed sound)

### Fixpoint Semantics

**Mathematical foundation**:

A Mangle program defines a **monotonic function** `T`:
```
T(Facts) = Facts ∪ { head | head :- body in program, body true in Facts }
```

**Fixpoint**: `T(S) = S`

**Least fixpoint**: Smallest set satisfying `T(S) = S`

**Theorem** (Tarski): Monotonic functions on finite domains have unique least fixpoint.

**Mangle computes** the least fixpoint via iteration:
```
S₀ = {}
S₁ = T(S₀)
S₂ = T(S₁)
...
Sₙ = T(Sₙ₋₁) = Sₙ  [fixpoint]
```

---

## Section 4: Mangle vs Alternatives

### vs Prolog

| Aspect | Mangle | Prolog |
|--------|--------|--------|
| **Evaluation** | Bottom-up | Top-down |
| **Termination** | Guaranteed (positive programs) | Can loop |
| **Aggregation** | Built-in transforms | Bagof/setof (awkward) |
| **Use case** | Data analysis | AI, expert systems |
| **Performance** | Predictable | Variable (depends on order) |

**When to use Prolog instead**:
- Need top-down search
- Backtracking is natural
- Infinite search spaces (with cuts)

### vs SQL

| Aspect | Mangle | SQL |
|--------|--------|-----|
| **Recursion** | Native, elegant | CTEs only, clunky |
| **Schema** | Schemaless | Requires schema |
| **Joins** | Implicit (via variables) | Explicit JOIN |
| **Negation** | Stratified semantics | NOT EXISTS/NOT IN |
| **Aggregation** | Transform pipelines | GROUP BY |

**Translation examples**:

```sql
-- SQL
SELECT category, COUNT(*) 
FROM items 
GROUP BY category;
```
```mangle
-- Mangle
count_per_cat(Cat, N) :- 
    item(Cat, _) |> 
    do fn:group_by(Cat), 
    let N = fn:Count().
```

**When to use SQL instead**:
- Need transactions
- Strong consistency required
- Massive scale (distributed DB)

### vs Pure Datalog

| Aspect | Mangle | Pure Datalog |
|--------|--------|--------------|
| **Aggregation** | ✅ fn:Sum, etc | ❌ Extensions only |
| **Structured data** | ✅ Maps, structs | ❌ Flat only |
| **Types** | ⚠️ Optional | ❌ None |
| **Transforms** | ✅ Pipeline | ❌ None |

**Mangle = Datalog + practical extensions**

### vs Z3/SMT Solvers

| Aspect | Mangle | Z3 |
|--------|--------|-----|
| **Purpose** | Data queries | Constraint solving |
| **Optimization** | ❌ | ✅ |
| **Performance** | Polynomial | NP-complete (can timeout) |
| **Use case** | "Find all X where..." | "Find X that optimizes..." |

**When to use Z3 instead**:
- Need optimization (maximize/minimize)
- Numerical constraints
- Theorem proving

---

## Section 5: Mental Models for Success

### Model 1: Relational Algebra

Think of Mangle as **relational algebra with recursion**:

```mangle
# Selection (σ in RA)
high_value(X) :- item(X, Value), Value > 1000.

# Projection (π in RA)
names_only(Name) :- person(_, Name, _).

# Join (⋈ in RA)
emp_dept(E, D) :- employee(E, DeptID), department(DeptID, D).

# Union (∪ in RA)
all_items(X) :- category1(X).
all_items(X) :- category2(X).

# Recursion (not in RA!)
closure(X, Y) :- edge(X, Y).
closure(X, Z) :- closure(X, Y), edge(Y, Z).
```

### Model 2: Graph Traversal

Mangle is a **graph query language**:
- Facts = nodes/edges
- Rules = traversal patterns
- Queries = reachability questions

```mangle
# "What nodes can I reach from X?"
reachable(Start, End) :- edge(Start, End).
reachable(Start, End) :- edge(Start, Mid), reachable(Mid, End).
```

### Model 3: Pattern Matching

Rules are **pattern → consequence**:

```mangle
# Pattern: "If there's a parent P of both X and Y, and X ≠ Y"
# Consequence: "Then X and Y are siblings"
sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.
```

### Model 4: Constraint Satisfaction

Rules express **constraints**:

```mangle
# Constraint: "All production servers must be running"
violation(Server) :- 
    server(Server, _, /production, Status),
    Status != /running.
```

---

## Conceptual Gotchas

### Gotcha 1: Closed-World Assumption

```mangle
# Just because it's not proven TRUE doesn't mean it's FALSE in reality
safe(X) :- project(X), not vulnerable(X).

# This means: "X is safe if we can't PROVE it's vulnerable"
# NOT: "X is safe if it's actually safe in the real world"
```

**Implication**: Incomplete data → incorrect conclusions.

### Gotcha 2: Set Semantics (No Duplicates)

```mangle
# Facts are a SET, not a BAG
parent(/a, /b).
parent(/a, /b).  # Duplicate, ignored

# Derived facts also de-duplicated
# If multiple rules derive sibling(/b, /c), stored only once
```

### Gotcha 3: No Ordering

```mangle
# These are IDENTICAL programs:
sibling(X, Y) :- parent(P, X), parent(P, Y).
sibling(X, Y) :- parent(P, Y), parent(P, X).

# Body atom order is a performance hint, not semantic
```

### Gotcha 4: Recursion is NOT Looping

```mangle
# This is NOT an infinite loop:
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- reachable(X, Y), edge(Y, Z).

# It's a DECLARATIVE SPEC of reachability
# Evaluation terminates at fixpoint
```

---

## Debugging Mental Model

When program doesn't work:

1. **Check stratification** (for negation)
   - Does negation create cycles?
   - Use ::show to see strata

2. **Check variable safety**
   - Are all head variables bound in body?
   - Are negated variables bound before negation?

3. **Check fixpoint**
   - Does evaluation terminate?
   - Are you deriving infinite facts?

4. **Check semantics**
   - What does the rule MEAN logically?
   - Would a human deduce the same conclusion?

---

**Next**: With these fundamentals, you're ready for [200-SYNTAX_REFERENCE](200-SYNTAX_REFERENCE.md) for complete language details.
