# 400: Recursion Mastery - Deep Dive

**Purpose**: Master every recursive technique for graph analysis, dependency tracking, and hierarchical data.

## Recursion Fundamentals

### Linear Recursion
```mangle
# Single recursive call per rule
ancestor(X, Y) :- parent(X, Y).
ancestor(X, Z) :- parent(X, Y), ancestor(Y, Z).
```

### Non-Linear Recursion
```mangle
# Multiple recursive paths
connected(X, Y) :- edge(X, Y).
connected(X, Y) :- edge(Y, X).  # Symmetric
connected(X, Z) :- connected(X, Y), connected(Y, Z).  # Transitive
```

## Path Construction

### Simple Paths
```mangle
path(X, Y, [X, Y]) :- edge(X, Y).
path(X, Z, [X|Rest]) :- edge(X, Y), path(Y, Z, Rest).
```

### Path with Metadata
```mangle
# Track length
path_len(X, Y, 1) :- edge(X, Y).
path_len(X, Z, Len) :- 
    edge(X, Y),
    path_len(Y, Z, SubLen) |>
    let Len = fn:plus(SubLen, 1).

# Track cost
path_cost(X, Y, Cost) :- edge(X, Y, Cost).
path_cost(X, Z, TotalCost) :- 
    edge(X, Y, Cost1),
    path_cost(Y, Z, Cost2) |>
    let TotalCost = fn:plus(Cost1, Cost2).
```

## Cycle Detection

### Back Edges
```mangle
cycle_edge(X, Y) :- edge(X, Y), reachable(Y, X).
```

### Has Cycle
```mangle
has_cycle(X) :- cycle_edge(X, _).
```

### Cycle-Free Paths
```mangle
# Prevent revisiting nodes
acyclic_path(X, Y, [X, Y]) :- edge(X, Y).
acyclic_path(X, Z, [X|Rest]) :- 
    edge(X, Y),
    acyclic_path(Y, Z, Rest),
    not member(X, Rest).  # X not in rest of path

member(X, [X|_]).
member(X, [_|Tail]) :- member(X, Tail).
```

## Distance & Optimization

### Shortest Path
```mangle
# All paths first
path_len(X, Y, Len) :- ...  # From above

# Then minimize
shortest(X, Y, MinLen) :- 
    path_len(X, Y, Len) |>
    do fn:group_by(X, Y),
    let MinLen = fn:Min(Len).
```

### Maximum Depth
```mangle
depth(Root, 0) :- root(Root).
depth(Node, D) :- 
    child(Parent, Node),
    depth(Parent, PD) |>
    let D = fn:plus(PD, 1).

max_depth(MaxD) :- 
    depth(_, D) |>
    let MaxD = fn:Max(D).
```

## Mutual Recursion

```mangle
# A and B defined in terms of each other
even(0).
even(N) :- N > 0, M = fn:minus(N, 1), odd(M).

odd(1).
odd(N) :- N > 1, M = fn:minus(N, 1), even(M).
```

## Transitive Patterns

### Dependency Closure
```mangle
depends(P, Lib) :- depends_direct(P, Lib).
depends(P, Lib) :- depends_direct(P, Q), depends(Q, Lib).
```

### Bill of Materials
```mangle
# Direct components
bom(Product, Part, Qty) :- assembly(Product, Part, Qty).

# Recursive components (multiply quantities)
bom(Product, Part, TotalQty) :- 
    assembly(Product, SubAssy, Qty1),
    bom(SubAssy, Part, Qty2) |>
    let TotalQty = fn:multiply(Qty1, Qty2).
```

## Tree Operations

### Subtree Size
```mangle
subtree_size(Node, 1) :- leaf(Node).
subtree_size(Node, Size) :- 
    child(Node, C),
    subtree_size(C, ChildSize) |>
    do fn:group_by(Node),
    let TotalChildren = fn:Sum(ChildSize),
    let Size = fn:plus(TotalChildren, 1).
```

### Topological Sort
```mangle
# Level 0: no dependencies
level(Node, 0) :- node(Node), not has_dependency(Node).
has_dependency(Node) :- depends_on(Node, _).

# Level N+1: all dependencies at level ≤ N
level(Node, Lev) :- 
    depends_on(Node, Dep),
    level(Dep, DepLev),
    not has_higher_dep(Node, DepLev) |>
    let Lev = fn:plus(DepLev, 1).

has_higher_dep(Node, L) :- 
    depends_on(Node, D),
    level(D, DL),
    DL > L.
```

## Termination Analysis

### Guaranteed Termination
✅ **Finite base facts** + **Monotonic rules** → Always terminates

### Potential Non-Termination
❌ **Unbounded recursion**:
```mangle
# Infinite growth (don't do this!)
count_up(N) :- count_up(M), N = fn:plus(M, 1).
```

### Safe Patterns
✅ **Bounded by existing data**:
```mangle
# Can't create new nodes, only traverse existing
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- reachable(X, Y), edge(Y, Z).
# Terminates when all paths explored
```

---

**See also**: 700-OPTIMIZATION.md for recursion performance tuning.
