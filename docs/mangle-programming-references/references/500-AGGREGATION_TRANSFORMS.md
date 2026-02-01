# 500: Aggregation & Transforms - Complete Guide

**Purpose**: Master transform pipelines and complex aggregations for analytics and data consolidation.

## Transform Pipeline Architecture

### Basic Pipeline
```mangle
result(Vars, Agg) :- 
    source_atoms |>
    do fn:operation() |>
    let Agg = fn:aggregate().
```

### Multi-Stage Pipeline
```mangle
result(Vars, Final) :- 
    source(X) |>
    do fn:filter(fn:gt(X, 10)) |>
    do fn:transform(X) |>
    do fn:group_by(Category) |>
    let Count = fn:Count() |>
    let Total = fn:Sum(Value) |>
    let Final = fn:divide(Total, Count).
```

## Aggregation Functions

### Count
```mangle
items_per_category(Cat, N) :- 
    item(Cat, _) |>
    do fn:group_by(Cat),
    let N = fn:Count().
```

### Sum, Min, Max
```mangle
category_stats(Cat, Total, Min, Max) :- 
    item(Cat, Value) |>
    do fn:group_by(Cat),
    let Total = fn:Sum(Value),
    let Min = fn:Min(Value),
    let Max = fn:Max(Value).
```

### Average (Derived)
```mangle
average_per_category(Cat, Avg) :- 
    item(Cat, Value) |>
    do fn:group_by(Cat),
    let Total = fn:Sum(Value),
    let Count = fn:Count(),
    let Avg = fn:divide(Total, Count).
```

## Conditional Aggregation

### Filter Before Aggregation
```mangle
high_value_count(Cat, N) :- 
    item(Cat, Value) |>
    do fn:filter(fn:gt(Value, 1000)),
    do fn:group_by(Cat),
    let N = fn:Count().
```

### Multiple Conditional Aggregations
```mangle
category_breakdown(Cat, HighCount, LowCount) :- 
    high_value_count(Cat, HighCount),
    low_value_count(Cat, LowCount).

high_value_count(Cat, N) :- 
    item(Cat, V) |> 
    do fn:filter(fn:gt(V, 1000)),
    do fn:group_by(Cat), 
    let N = fn:Count().

low_value_count(Cat, N) :- 
    item(Cat, V) |> 
    do fn:filter(fn:le(V, 1000)),
    do fn:group_by(Cat), 
    let N = fn:Count().
```

## Multi-Dimensional Grouping

```mangle
# Group by multiple variables
sales_summary(Region, Product, Count, Revenue) :- 
    sale(Region, Product, Amount) |>
    do fn:group_by(Region, Product),
    let Count = fn:Count(),
    let Revenue = fn:Sum(Amount).
```

## Nested Aggregation

### Aggregate Then Aggregate
```mangle
# Step 1: Category totals
category_total(Cat, Total) :- 
    item(Cat, Value) |>
    do fn:group_by(Cat),
    let Total = fn:Sum(Value).

# Step 2: Overall statistics from category totals
overall_stats(GrandTotal, AvgCategoryTotal) :- 
    category_total(_, Total) |>
    do fn:group_by(),
    let GrandTotal = fn:Sum(Total),
    let Count = fn:Count(),
    let AvgCategoryTotal = fn:divide(GrandTotal, Count).
```

## Window Functions (Simulated)

### Running Total
```mangle
# Ordered by assuming natural order
running_total(Item, RunningSum) :- 
    item(Item, Value),
    item(PrevItem, PrevValue),
    PrevItem < Item |>  # Ordering assumption
    do fn:group_by(Item),
    let RunningSum = fn:Sum(PrevValue).
```

### Rank (Simulated)
```mangle
# Count items with higher value
rank(Item, Rank) :- 
    item(Item, Value),
    item(OtherItem, OtherValue),
    OtherValue > Value |>
    do fn:group_by(Item),
    let Rank = fn:plus(fn:Count(), 1).
```

---

**See also**: 300-PATTERN_LIBRARY.md for aggregation pattern examples.
