# 200: Complete Syntax Reference

**Purpose**: Definitive language specification. Every construct, operator, function, and rule.

## Quick Lookup Table

| Construct | Syntax | Example | Section |
|-----------|--------|---------|---------|
| **Facts** | `pred(args).` | `parent(/a, /b).` | 2.1 |
| **Rules** | `head :- body.` | `sib(X,Y) :- parent(P,X), parent(P,Y).` | 2.2 |
| **Queries** | `?pred(args)` | `?sibling(X, Y)` | 2.3 |
| **Names** | `/identifier` | `/oedipus`, `/critical` | 3.1 |
| **Variables** | `UPPERCASE` | `X`, `Person`, `Count` | 3.2 |
| **Numbers** | `-?\d+(\.\d+)?` | `42`, `-17`, `3.14` | 3.3 |
| **Strings** | `"text"` | `"log4j"`, `"CVE-2021"` | 3.4 |
| **Lists** | `[T, ...]` | `[1, 2, 3]`, `[/a]` | 3.5 |
| **Maps** | `{/k: v}` | `{/name: "Alice"}` | 3.6 |
| **Negation** | `not atom` | `not excluded(X)` | 4.3 |
| **Comparison** | `X op Y` | `X != Y`, `X > 100` | 4.5 |
| **Transform** | `... \|> ...` | `data \|> fn:Count()` | 5 |
| **Grouping** | `fn:group_by(V)` | `fn:group_by(Cat)` | 5.2 |
| **Aggregation** | `fn:Sum(V)` | `let Total = fn:Sum(Value)` | 5.3 |

---

## 1. Program Structure

### 1.1 Overall Organization

```mangle
# 1. Optional type declarations
Decl predicate(Arg1.Type<type1>, Arg2.Type<type2>).

# 2. Base facts (EDB - Extensional Database)
fact_name(constant1, constant2).

# 3. Rules (IDB - Intensional Database)
derived(X) :- base(X), condition(X).

# 4. Queries (interactive REPL only)
?derived(X)
```

### 1.2 File Format

**Extension**: `.mg` or `.mangle`
**Encoding**: UTF-8
**Line endings**: Any (Unix/Windows/Mac)
**Whitespace**: Ignored except in strings

---

## 2. Core Constructs

### 2.1 Facts (Ground Atoms)

**Syntax**: `predicate_symbol(term1, term2, ..., termN).`

**Examples**:
```mangle
parent(/oedipus, /antigone).
vulnerable("log4j", "2.14.0", "CVE-2021-44228").
contains_jar(/project1, /jar_name, "1.0.0").
age(/alice, 30).
```

**Rules**:
- Must end with period `.`
- All terms must be ground (no variables)
- Predicate symbol: lowercase start
- Arity (number of arguments) is part of identity

### 2.2 Rules (Horn Clauses)

**Syntax**: `head :- body1, body2, ..., bodyN.`

**Synonyms**: `:−` or `⟸` (Unicode alternative to `:-`)

**Components**:
```mangle
# head :- body_atom1, body_atom2, ...
sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.
#  ↑              ↑              ↑              ↑
# head         body atom 1    body atom 2   body atom 3
```

**Semantics**: "Head is true if all body atoms are true"

**Multi-line rules allowed**:
```mangle
vulnerable_project(Project, CVE, Severity) :- 
    project(Project),
    depends_on(Project, Lib, Version),
    cve_affects(Lib, Version, CVE, Severity).
```

**Multiple rules for same predicate** (union semantics):
```mangle
result(X) :- source1(X).
result(X) :- source2(X).
result(X) :- source3(X).
# result = source1 ∪ source2 ∪ source3
```

### 2.3 Queries

**Interactive REPL only** (not in `.mg` files)

**Syntax**: `?predicate(pattern)`

**Examples**:
```mangle
# Variable pattern
?sibling(X, Y)              # Find all siblings

# Constant pattern
?parent(/oedipus, /antigone) # Check if true

# Mixed pattern
?parent(/oedipus, X)         # Find oedipus's children
```

**REPL returns**:
- All matching facts
- Variable bindings
- Or "No results" if none match

---

## 3. Data Types

### 3.1 Names (Atoms)

**Syntax**: `/identifier`

**Identifier rules**:
- Start with letter or underscore
- Contains letters, digits, underscores
- Regex: `/[a-zA-Z_][a-zA-Z0-9_]*`

**Examples**:
```mangle
/oedipus
/critical_severity
/production_2024
/web_app
/us_east_1
```

**Case-sensitive**: `/Alice` ≠ `/alice`

### 3.2 Variables

**Syntax**: Starts with UPPERCASE

**Examples**:
```mangle
X
Person
ProjectName
Count
TotalValue
```

**Scoping**:
- Variables scoped to single rule
- Same variable in different rules is DIFFERENT variable

**Anonymous variable**: `_` (matches anything, not bound)
```mangle
# Ignore middle argument
first_and_last(A, C) :- triple(A, _, C).
```

### 3.3 Numbers

**Integers**:
```mangle
42
-17
0
1000000
```
**Range**: 64-bit signed integer

**Floats**:
```mangle
3.14
-2.5
1.0e6
-3.7e-10
```
**Format**: 64-bit IEEE 754

### 3.4 Strings

**Syntax**: `"characters"`

**Escaping**:
```mangle
"normal string"
"with \"quotes\""
"with\nnewline"
"with\ttab"
"with\\backslash"
```

**Supported escapes**: `\"`, `\n`, `\t`, `\\`

### 3.5 Lists

**Syntax**: `[elem1, elem2, ..., elemN]`

**Examples**:
```mangle
[1, 2, 3]
[/a, /b, /c]
["one", "two"]
[]                  # Empty list
[[1, 2], [3, 4]]    # Nested
```

**Homogeneous preferred** (all same type)

**List construction**:
```mangle
# Build list in transform
path(Start, End, [Start, End]) :- edge(Start, End).

# Cons operator: [Head|Tail]
path(Start, End, [Start|Rest]) :- 
    edge(Start, Mid),
    path(Mid, End, Rest).
```

### 3.6 Maps & Structs

**Syntax**: `{/key1: value1, /key2: value2}`

**Maps** (generic key-value):
```mangle
{/name: "Alice", /age: 30}
{/x: 10, /y: 20}
```

**Structs** (fixed schema, semantically equivalent to maps):
```mangle
person_record(1, {/name: "Alice", /age: 30, /city: "NYC"}).
```

**Access**:
```mangle
# Via :match_field
record_name(ID, Name) :- 
    person_record(ID, Info),
    :match_field(Info, /name, Name).

# Via :match_entry (same semantics)
record_name(ID, Name) :- 
    person_record(ID, Info),
    :match_entry(Info, /name, Name).
```

---

## 4. Operators

### 4.1 Rule Implication

**Operators**: `:−` or `⟸` (Unicode)

**Associativity**: Right

**Meaning**: "If body, then head"

### 4.2 Conjunction (AND)

**Operator**: `,`

**Associativity**: Left

**Meaning**: ALL atoms must be true

```mangle
# X and Y and Z all must hold
rule(A, B, C) :- cond1(A), cond2(B), cond3(C).
```

### 4.3 Negation

**Operator**: `not`

**Position**: Prefix (before atom)

**Example**:
```mangle
safe(X) :- candidate(X), not excluded(X).
```

**Safety constraint**: Variables in negated atom must be bound by positive atoms before negation.

```mangle
# ✅ SAFE
safe(X) :- item(X), not excluded(X).  # X bound by item

# ❌ UNSAFE
bad(X) :- not foo(X).  # X not bound
```

**Stratification required** (see 100-FUNDAMENTALS.md)

### 4.4 Unification

**Operator**: `=`

**Meaning**: Make two terms equal (bind variables)

```mangle
# Bind X to 42
rule(X) :- condition(Y), X = 42.

# Pattern matching
rule(X, Y) :- data({/a: X, /b: Y}).
```

### 4.5 Comparison

**Operators**:
| Operator | Meaning | Types |
|----------|---------|-------|
| `=` | Unification | All |
| `!=` | Inequality | All |
| `<` | Less than | Numeric |
| `<=` | Less or equal | Numeric |
| `>` | Greater than | Numeric |
| `>=` | Greater or equal | Numeric |

**Examples**:
```mangle
adult(X) :- person(X), age(X, A), A >= 18.
different(X, Y) :- item(X), item(Y), X != Y.
large(X) :- size(X, S), S > 1000.
```

**Type restrictions**:
- Numeric operators: integers and floats only
- Equality/inequality: any types

### 4.6 Pipeline

**Operator**: `|>`

**Purpose**: Chain transforms (see Section 5)

**Position**: Infix

```mangle
data(X) |> 
    do fn:transform() |> 
    let Result = fn:aggregate().
```

---

## 5. Transforms & Aggregation

### 5.1 Transform Syntax

**General form**:
```mangle
result(Vars, AggResults) :- 
    body_atoms |>
    do fn:transform1() |>
    do fn:transform2() |>
    let AggVar1 = fn:aggregate1(),
    let AggVar2 = fn:aggregate2().
```

**Keywords**:
- `|>` - Pipeline operator
- `do` - Apply transform function
- `let` - Bind aggregation result

### 5.2 Grouping

**Function**: `fn:group_by(Var1, Var2, ...)`

**Purpose**: Partition facts by grouping variables

```mangle
# Group by single variable
count_per_category(Cat, N) :- 
    item(Cat, _) |> 
    do fn:group_by(Cat), 
    let N = fn:Count().

# Group by multiple variables
stats(Region, Product, Count) :- 
    sale(Region, Product, Amount) |> 
    do fn:group_by(Region, Product), 
    let Count = fn:Count().
```

### 5.3 Aggregation Functions

**Count**:
```mangle
fn:Count()  # Count elements in group
```

**Sum**:
```mangle
fn:Sum(Variable)  # Sum numeric values
```

**Min/Max**:
```mangle
fn:Min(Variable)  # Minimum value
fn:Max(Variable)  # Maximum value
```

**Example - all aggregations**:
```mangle
category_stats(Cat, Count, Total, Avg, Min, Max) :- 
    item(Cat, Value) |> 
    do fn:group_by(Cat), 
    let Count = fn:Count(),
    let Total = fn:Sum(Value),
    let Min = fn:Min(Value),
    let Max = fn:Max(Value) |>
    let Avg = fn:divide(Total, Count).
```

### 5.4 Arithmetic Functions

**Basic operations**:
```mangle
fn:plus(A, B)      # A + B
fn:minus(A, B)     # A - B
fn:multiply(A, B)  # A × B
fn:divide(A, B)    # A / B
fn:modulo(A, B)    # A % B
fn:negate(A)       # -A
fn:abs(A)          # |A|
```

**Usage in transforms**:
```mangle
# Calculate average
average(Cat, Avg) :- 
    item(Cat, Value) |> 
    do fn:group_by(Cat), 
    let Total = fn:Sum(Value),
    let Count = fn:Count(),
    let Avg = fn:divide(Total, Count).
```

### 5.5 Comparison Functions

**Functions**:
```mangle
fn:eq(A, B)   # A = B
fn:ne(A, B)   # A ≠ B
fn:lt(A, B)   # A < B
fn:le(A, B)   # A ≤ B
fn:gt(A, B)   # A > B
fn:ge(A, B)   # A ≥ B
```

**Usage in filter transforms**:
```mangle
high_values(Cat, N) :- 
    item(Cat, Value) |> 
    do fn:filter(fn:gt(Value, 1000)),
    do fn:group_by(Cat), 
    let N = fn:Count().
```

### 5.6 Data Structure Functions

**Struct/Map access**:
```mangle
:match_field(Struct, /field_name, Value)
:match_entry(Map, /key, Value)
```

**List operations**:
```mangle
fn:list_cons(Head, Tail)        # [Head|Tail]
fn:list_append(List1, List2)    # List1 ++ List2
fn:list_length(List)             # Length
```

**String operations**:
```mangle
fn:string_concat(S1, S2)         # S1 + S2
fn:string_length(S)              # Length
fn:string_contains(S, Substring) # Contains check
```

---

## 6. Type System

### 6.1 Type Declarations

**Syntax**: `Decl predicate(Arg.Type<type>).`

**Example**:
```mangle
Decl employee(ID.Type<int>, Name.Type<string>, Dept.Type<n>).
Decl salary(ID.Type<int>, Amount.Type<float>).
```

**Type syntax**:
```mangle
Type<int>              # Integer
Type<float>            # Float
Type<string>           # String
Type<n>                # Name (atom)
Type<[T]>              # List of T
Type<{/k: v}>          # Map
Type<T1 | T2>          # Union type
Type<Any>              # Any type
```

### 6.2 Gradual Typing

**Optional**: Types can be omitted
```mangle
# No declaration needed
employee(1, "Alice", /engineering).
```

**Type checking**: At runtime when declarations exist

**Type inference**: From usage patterns

### 6.3 Structured Types

**List types**:
```mangle
Decl tags(ID.Type<int>, Tags.Type<[string]>).

tags(1, ["critical", "urgent"]).
```

**Map types**:
```mangle
Decl config(Data.Type<{/host: string, /port: int}>).

config({/host: "localhost", /port: 8080}).
```

**Union types**:
```mangle
Decl flexible(Value.Type<int | string>).

flexible(42).
flexible("text").
```

---

## 7. Safety Constraints

### 7.1 Variable Safety

**Rule**: Every variable in rule head must appear in:
1. A positive body atom, OR
2. A unification `Var = constant`

**Examples**:
```mangle
# ✅ SAFE
rule(X, Y) :- foo(X), bar(Y).           # Both X, Y bound
rule(X, Y) :- foo(X), Y = 42.            # X bound by atom, Y by unification

# ❌ UNSAFE
rule(X, Y) :- foo(X).                    # Y unbound
```

### 7.2 Negation Safety

**Rule**: Variables in negated atom must be bound by positive atoms BEFORE negation.

**Examples**:
```mangle
# ✅ SAFE
safe(X) :- candidate(X), not excluded(X).  # X bound by candidate

# ❌ UNSAFE
unsafe(X) :- not foo(X).                   # X never bound
```

### 7.3 Aggregation Safety

**Rule**: Grouping variables must appear in body atoms.

```mangle
# ✅ SAFE
count_per_cat(Cat, N) :- 
    item(Cat, _) |>          # Cat appears in body
    do fn:group_by(Cat),      # Group by Cat
    let N = fn:Count().

# ❌ UNSAFE
bad(Cat, N) :- 
    item(_, _) |>             # Cat never appears
    do fn:group_by(Cat),      # Can't group by unbound
    let N = fn:Count().
```

---

## 8. Evaluation Model

### 8.1 Bottom-Up Semi-Naive

**Algorithm**:
```
Δ₀ = EDB (base facts)
For each stratum S (in order):
    i = 0
    repeat:
        Δᵢ₊₁ = apply rules to Δᵢ (using all facts)
        Δᵢ₊₁ = Δᵢ₊₁ \ (all previously derived facts)
        i++
    until Δᵢ = ∅ (fixpoint)
```

**Key**: Only NEW facts trigger re-evaluation.

### 8.2 Stratification

**For programs with negation**:

1. Compute dependency graph
2. Find strongly connected components
3. Topologically sort
4. Ensure negation edges go backward only
5. Evaluate strata in order

**Example**:
```mangle
# Stratum 0
base(X) :- source(X).

# Stratum 1 (positive dependency on 0)
derived(X) :- base(X), cond(X).

# Stratum 2 (negative dependency on 1)
final(X) :- base(X), not derived(X).
```

**Evaluation**: Stratum 0 → fixpoint, then Stratum 1 → fixpoint, then Stratum 2 → fixpoint.

---

## 9. Comments

**Syntax**: `#` to end of line

**Examples**:
```mangle
# Single line comment

parent(/a, /b).  # Inline comment

# Multi-line "comment":
# Just use multiple
# single-line comments
```

**No block comments** (`/* */` not supported).

---

## 10. REPL Commands

**Interactive interpreter commands**:

| Command | Effect |
|---------|--------|
| `<decl>.` | Add type declaration |
| `<clause>.` | Add clause (fact/rule), evaluate |
| `?<atom>` | Query predicate |
| `::load <path>` | Load source file |
| `::help` | Show help |
| `::pop` | Reset to previous state |
| `::show <pred>` | Show predicate info |
| `::show all` | Show all predicates |
| `Ctrl-D` | Exit REPL |

**Examples**:
```
# Load program
::load vulnerability_scanner.mg

# Query
?vulnerable_project(P, CVE, Sev)

# Show predicate info
::show vulnerable_project

# Exit
Ctrl-D
```

---

## 11. Grammar Summary

**Complete EBNF**:
```ebnf
Program     ::= (Decl | Clause)*
Decl        ::= 'Decl' Atom '.'
Clause      ::= Atom (':-' Atom (',' Atom)*)? '.'
Atom        ::= PredicateSym '(' Term (',' Term)* ')'
             |  'not' Atom
             |  Term Op Term
Term        ::= Const | Var | List | Map | Transform
Const       ::= Name | Int | Float | String
Name        ::= '/' Identifier
Var         ::= UppercaseIdentifier
List        ::= '[' (Term (',' Term)*)? ']'
Map         ::= '{' (Name ':' Term (',' Name ':' Term)*)? '}'
Transform   ::= Term '|>' TransformOp
TransformOp ::= 'do' Function | 'let' Var '=' Function
Op          ::= '=' | '!=' | '<' | '<=' | '>' | '>='
```

---

**Next**: With complete syntax knowledge, see [300-PATTERN_LIBRARY](300-PATTERN_LIBRARY.md) for comprehensive pattern catalog.
