# 600: Type System - Types, Lattices, and Gradual Typing

**Purpose**: Understand Mangle's optional type system, structured data types, and experimental lattice support.

## Type Declarations

### Basic Types
```mangle
Decl employee(ID.Type<int>, Name.Type<string>, Dept.Type<n>).
Decl salary(EmpID.Type<int>, Amount.Type<float>).
```

### Type Syntax
```mangle
Type<int>              # 64-bit integer
Type<float>            # 64-bit float
Type<string>           # UTF-8 string
Type<n>                # Name (atom)
Type<[T]>              # List of type T
Type<{/k: v}>          # Map/struct
Type<T1 | T2>          # Union type
Type<Any>              # Any type
```

## Structured Types

### Lists
```mangle
Decl tags(ID.Type<int>, Tags.Type<[string]>).

tags(1, ["urgent", "critical"]).
tags(2, ["low-priority"]).
```

### Maps/Structs
```mangle
Decl person(ID.Type<int>, Info.Type<{/name: string, /age: int}>).

person(1, {/name: "Alice", /age: 30}).

# Access fields
person_name(ID, Name) :- 
    person(ID, Info),
    :match_field(Info, /name, Name).
```

### Union Types
```mangle
Decl flexible_value(ID.Type<int>, Val.Type<int | string>).

flexible_value(1, 42).
flexible_value(2, "text").
```

## Gradual Typing

**Optional**: Can omit declarations
```mangle
# No declaration needed
data(1, "test").
data(2, 42).
# Mangle infers types from usage
```

**Type checking**: At runtime when types declared
**Type inference**: From usage patterns

## Lattice Support (Experimental)

**Concept**: Maintain only maximal elements under partial order

**Use cases**:
- Interval analysis (track min/max bounds)
- Provenance (track minimal sources)
- Abstract interpretation

**Status**: Experimental feature, check Mangle releases for availability

---

**See also**: 200-SYNTAX_REFERENCE.md Section 6 for complete type syntax.
