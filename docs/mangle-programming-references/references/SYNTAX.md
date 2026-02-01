# Complete Mangle Syntax Reference

## Data Types

### Base Types
- **Names/Atoms**: `/symbol` (e.g., `/oedipus`, `/production`, `/critical`)
- **Integers**: `42`, `-17`, `1000000`
- **Floats**: `3.14`, `-2.5`, `1.0e6`
- **Strings**: `"log4j"`, `"CVE-2021-44228"`

### Structured Types
- **Lists**: `[1, 2, 3]`, `[/a, /b]`, `[]` (empty)
- **Maps**: `{/key1: value1, /key2: value2}`
- **Structs**: `{/name: "Alice", /age: 30}`

## Syntax Elements

### Facts
```mangle
predicate(arg1, arg2, arg3).
parent(/tereus, /itys).
contains_jar(/project, "log4j", "2.14.0").
```

### Rules
```mangle
# Basic rule
head(X, Y) :- body1(X), body2(Y).

# Rule with multiple conditions
sibling(X, Y) :- 
    parent(P, X), 
    parent(P, Y), 
    X != Y.

# Recursive rule
reachable(X, Z) :- edge(X, Y), reachable(Y, Z).
```

### Queries
```mangle
# Pattern matching
?predicate(/constant, Variable)

# Lookup
?parent(/oedipus, X)
```

### Declarations (Optional Type Annotations)
```mangle
Decl employee(ID.Type<int>, Name.Type<string>, Dept.Type<n>).
Decl salary(ID.Type<int>, Amount.Type<float>).
```

## Operators

### Logical
- `:−` or `⟸` - Rule implication ("if")
- `,` - Conjunction (AND)
- `not` - Negation (requires stratification)

### Comparison
- `=` - Unification
- `!=` - Inequality
- `<`, `<=`, `>`, `>=` - Numeric comparisons

### Transform Pipeline
- `|>` - Pipe operator for chaining transformations

## Built-in Functions

### Arithmetic
```mangle
fn:plus(A, B)      # Addition
fn:minus(A, B)     # Subtraction
fn:multiply(A, B)  # Multiplication
```

### Aggregation
```mangle
fn:Count()         # Count elements
fn:Sum(Var)        # Sum numeric values
fn:Max(Var)        # Maximum value
fn:Min(Var)        # Minimum value
fn:group_by(V...)  # Group by variables
```

### Data Structure Access
```mangle
:match_field(Struct, /field_name, Value)  # Access struct field
:match_entry(Map, /key, Value)             # Access map entry
```

## Transform Syntax

### Basic Transform
```mangle
result(Count) :- 
    data(X) |> 
    do fn:group_by(), 
    let Count = fn:Count().
```

### Multi-stage Transform
```mangle
stats(Category, Total) :- 
    item(Category, Value) |> 
    do fn:filter(fn:gt(Value, 10)),
    do fn:group_by(Category), 
    let Total = fn:Sum(Value).
```

## Variable Naming

### Conventions
- **Variables**: Start with uppercase (`X`, `Person`, `Count`)
- **Constants**: Names start with `/` (`/oedipus`, `/critical`)
- **Strings**: Double quotes (`"text"`)

### Safety Rules
All variables in rule head must appear in positive body atoms:

```mangle
# ✅ SAFE
safe(X, Y) :- foo(X), bar(Y).

# ❌ UNSAFE
unsafe(X, Y) :- foo(X).  # Y unbound

# ✅ SAFE with constant
also_safe(X, Y) :- foo(X), Y = 42.
```

## Negation Safety

Variables in negated atoms must be bound by positive atoms:

```mangle
# ✅ SAFE
safe(X) :- candidate(X), not excluded(X).

# ❌ UNSAFE
unsafe(X) :- not foo(X).  # X not bound first
```

## Comments

```mangle
# Single line comment

# Multi-line comments:
# Just use multiple single-line comments
# like this
```

## Special Syntax

### List Construction
```mangle
route_list(Codes) :- 
    connections(C1, C2) |> 
    let Codes = [C1, C2].
```

### Struct Creation
```mangle
person_record(Record) :- 
    person(Name, Age) |> 
    let Record = {/name: Name, /age: Age}.
```

## Evaluation Model

**Semi-naive bottom-up evaluation**:
1. Start with base facts
2. Apply rules to derive new facts
3. Repeat until no new facts (fixpoint)
4. Only process NEW facts each iteration (semi-naive optimization)

**Stratification** (for negation):
1. Compute dependency strata
2. Evaluate each stratum to fixpoint
3. Results immutable for higher strata

## Interactive Interpreter Commands

```
<decl>.              # Add declaration
<clause>.            # Add clause, evaluate
?<predicate>         # Look up facts
?<goal>             # Query with pattern
::load <path>        # Load source file
::help              # Show help
::pop               # Reset to previous state
::show <predicate>  # Show predicate info
::show all          # Show all predicates
<Ctrl-D>            # Quit
```
