# 800: Mathematical Foundations & Theory

**Purpose**: Deep mathematical underpinnings of Mangle for researchers and theorists.

## First-Order Logic Foundations

### Syntax
Mangle programs are **first-order Horn clauses**:
```
∀X, Y: ancestor(X, Y) ← parent(X, Y)
∀X, Y, Z: ancestor(X, Z) ← parent(X, Y) ∧ ancestor(Y, Z)
```

### Semantics
**Herbrand semantics**: Interpretation over ground terms

**Model**: Set of ground atoms that satisfy all rules

**Minimal model**: Smallest set closed under rules

## Fixed-Point Semantics

### Immediate Consequence Operator

**Definition**: T_P(I) = {head | head :- body in P, body true in I}

**Properties**:
- Monotonic: I ⊆ J → T_P(I) ⊆ T_P(J)
- Continuous (finite case)

### Least Fixpoint

**Theorem (Tarski)**: Monotonic functions on complete lattices have unique least fixpoint.

**Computation**:
```
lfp(T_P) = T_P^ω(∅) = ∪_{i=0}^∞ T_P^i(∅)
```

**Convergence**: Finite in finite time (finite Herbrand base)

## Stratified Semantics

### Dependency Graph
- Nodes: Predicates
- Edges: Positive (p uses q) or Negative (p uses ¬q)

### Stratification
**Definition**: Partition predicates into strata S₀, S₁, ..., Sₙ such that:
- Positive edges: within or forward strata
- Negative edges: only backward (higher to lower strata)

**Perfect model**: Evaluate strata bottom-up, each to fixpoint

## Complexity

### Data Complexity
**Positive Datalog**: P-complete
**Stratified Datalog**: P-complete

**Query evaluation**: Polynomial in database size

### Combined Complexity
**Positive Datalog**: EXPTIME-complete
**Stratified Datalog**: EXPTIME-complete

(Considering both program and data size)

## Comparison with Datalog Variants

| Variant | Features | Complexity |
|---------|----------|------------|
| **Pure Datalog** | Horn clauses only | P |
| **Stratified Datalog** | + Negation (stratified) | P |
| **Datalog¬** | + Unstratified negation | coNP |
| **Datalog^Z** | + Integers, arithmetic | Undecidable |
| **Mangle** | Stratified + aggregation | P (data) |

## Academic References

**Foundations**:
- Abiteboul, Hull, Vianu: "Foundations of Databases" (1995)
- Ullman: "Principles of Database Systems" (1988)

**Stratified negation**:
- Apt, Blair, Walker: "Towards a theory of declarative knowledge" (1988)

**Fixed-point semantics**:
- Van Emden, Kowalski: "The Semantics of Predicate Logic as a Programming Language" (1976)

**Complexity**:
- Dantsin et al: "Complexity and expressive power of logic programming" (2001)

---

**This concludes the theoretical foundations. Return to practical guides for implementation.**
