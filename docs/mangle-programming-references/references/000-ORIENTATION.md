# 000: Library Orientation & Navigation Guide

**Purpose**: Navigate the Mangle encyclopedia efficiently. Find exactly what you need without information overload.

## The Dewey Decimal System for Mangle

This skill uses **progressive disclosure** through a numbered classification system:

```
000-099  → Orientation & Navigation (you are here)
100-199  → Fundamentals & Theory
200-299  → Language Reference & Syntax
300-399  → Pattern Libraries & Catalogs
400-499  → Recursion Techniques
500-599  → Aggregation & Transforms
600-699  → Type Systems & Lattices
700-799  → Optimization & Performance
800-899  → Mathematical Foundations
900-999  → Ecosystem & Tooling
```

## Navigation Patterns

### By Experience Level

**Absolute Beginner (Never written Mangle code)**
1. SKILL.md → Quick Start (5 min)
2. 100-FUNDAMENTALS.md → Sections 1-2 (30 min)
3. 300-PATTERN_LIBRARY.md → Basic Patterns (30 min)
4. Install & run first program (15 min)

**Familiar with Logic Programming (Prolog/Datalog)**
1. SKILL.md → Comparison table
2. 100-FUNDAMENTALS.md → Section 4 (Mangle vs Others)
3. 200-SYNTAX_REFERENCE.md → Scan for differences
4. 500-AGGREGATION_TRANSFORMS.md → Transform syntax (Mangle-specific)

**Coming from SQL Background**
1. SKILL.md → Quick Start
2. 300-PATTERN_LIBRARY.md → SQL equivalents section
3. 500-AGGREGATION_TRANSFORMS.md → GROUP BY equivalents
4. 400-RECURSION_MASTERY.md → Recursive CTEs equivalent

**Production Deployment Focus**
1. 900-ECOSYSTEM.md → Architecture patterns
2. 700-OPTIMIZATION.md → Performance engineering
3. PRODUCTION.md → Deployment guide
4. 900-ECOSYSTEM.md → Monitoring

### By Use Case

**Vulnerability Detection**
→ EXAMPLES.md (Example 2: SBOM Analyzer)
→ 400-RECURSION_MASTERY.md (Transitive dependencies)
→ 300-PATTERN_LIBRARY.md (Recursive patterns)
→ 900-ECOSYSTEM.md (Production deployment)

**Graph Analysis**
→ 400-RECURSION_MASTERY.md (All sections)
→ 300-PATTERN_LIBRARY.md (Path finding, reachability)
→ 700-OPTIMIZATION.md (Large graph performance)

**Policy Compliance**
→ EXAMPLES.md (Example 4: Infrastructure policy)
→ 300-PATTERN_LIBRARY.md (Negation patterns)
→ 600-TYPE_SYSTEM.md (Data validation)

**Multi-Source Integration**
→ ADVANCED_PATTERNS.md (Multi-source patterns)
→ 600-TYPE_SYSTEM.md (Structured data)
→ 500-AGGREGATION_TRANSFORMS.md (Data consolidation)

### By Topic

**Need help with...**

| Topic | Primary Reference | Supporting |
|-------|------------------|------------|
| Syntax errors | 200-SYNTAX_REFERENCE | SKILL.md (Pitfalls) |
| Recursion not working | 400-RECURSION_MASTERY | 100-FUNDAMENTALS (Evaluation) |
| Aggregation | 500-AGGREGATION_TRANSFORMS | 300-PATTERN_LIBRARY |
| Performance issues | 700-OPTIMIZATION | PRODUCTION.md |
| Negation errors | 200-SYNTAX_REFERENCE (Safety) | 100-FUNDAMENTALS (Stratification) |
| Type errors | 600-TYPE_SYSTEM | 200-SYNTAX_REFERENCE |
| Go integration | 900-ECOSYSTEM | PRODUCTION.md |
| Theory/semantics | 800-THEORY | 100-FUNDAMENTALS |

## Reference Architecture

### Core Files (Read First)

**SKILL.md** (5-10 min)
- Entry point for all users
- Quick Start, common patterns, navigation
- Read FIRST always

**000-ORIENTATION.md** (you are here, 5 min)
- How to navigate the library
- Learning paths
- Quick lookup guide

### Numbered References (Deep Dives)

Each reference follows this structure:
1. **Quick Reference** - Scan in 30 seconds
2. **Core Content** - Read in 10-30 minutes
3. **Advanced Topics** - Study in 1-2 hours
4. **Examples** - Apply immediately

### Legacy Files (Being Migrated)

- SYNTAX.md → Migrating to 200-SYNTAX_REFERENCE.md
- EXAMPLES.md → Migrating to 300-PATTERN_LIBRARY.md
- ADVANCED_PATTERNS.md → Migrating to 400/500/700
- PRODUCTION.md → Migrating to 900-ECOSYSTEM.md

**Current status**: Use both. Numbered references are more comprehensive.

## Reading Strategies

### Strategy 1: Just-in-Time Learning
1. Encounter problem
2. Check this guide (Navigation Patterns → By Topic)
3. Go directly to relevant section
4. Read only that section
5. Return when needed

### Strategy 2: Foundation Building
1. SKILL.md (full)
2. 100-FUNDAMENTALS.md (sections 1-3)
3. 200-SYNTAX_REFERENCE.md (scan)
4. 300-PATTERN_LIBRARY.md (Basic Patterns)
5. Build first program
6. Expand as needed

### Strategy 3: Deep Mastery
1. Complete Strategy 2
2. Read 100-900 sequentially
3. Study 800-THEORY
4. Read academic papers in 800-THEORY
5. Contribute to github.com/google/mangle

## Quick Lookup Guide

### "I need to..."

**Write my first program**
→ SKILL.md → Quick Start

**Understand why Mangle exists**
→ 100-FUNDAMENTALS.md → Section 1

**Find every syntax rule**
→ 200-SYNTAX_REFERENCE.md

**Copy-paste a pattern**
→ 300-PATTERN_LIBRARY.md

**Make recursion work**
→ 400-RECURSION_MASTERY.md

**Use GROUP BY equivalent**
→ 500-AGGREGATION_TRANSFORMS.md

**Declare types**
→ 600-TYPE_SYSTEM.md

**Optimize slow program**
→ 700-OPTIMIZATION.md

**Understand the math**
→ 800-THEORY.md

**Deploy to production**
→ 900-ECOSYSTEM.md

## Information Density Philosophy

Each reference optimizes for different goals:

- **SKILL.md**: Maximum practical value, minimum reading time
- **000-ORIENTATION**: Navigation efficiency
- **100-FUNDAMENTALS**: Conceptual foundations
- **200-SYNTAX**: Completeness (every rule)
- **300-PATTERN**: Comprehensiveness (every pattern)
- **400-500-600**: Deep mastery of specific topics
- **700-OPTIMIZATION**: Actionable performance gains
- **800-THEORY**: Mathematical rigor
- **900-ECOSYSTEM**: Production readiness

## When to Consult Each Reference

### During Development

**Writing rules**: Keep 200-SYNTAX_REFERENCE open
**Implementing patterns**: Keep 300-PATTERN_LIBRARY open
**Debugging**: Check 700-OPTIMIZATION (common issues)
**Type errors**: Check 600-TYPE_SYSTEM

### Before Development

**Architecture decisions**: 900-ECOSYSTEM
**Feasibility**: 100-FUNDAMENTALS + SKILL.md comparison table
**Learning new technique**: Relevant 400-800 reference

### After Development

**Optimization**: 700-OPTIMIZATION
**Production hardening**: 900-ECOSYSTEM + PRODUCTION.md
**Testing**: PRODUCTION.md (testing section)

## Skill Maintenance

This skill is designed to be:
1. **Self-contained**: All critical info included
2. **Progressive**: Start simple, add depth as needed
3. **Complete**: Every Mangle feature documented
4. **Practical**: Copy-paste ready examples

## Next Steps

**If you're new**: Go to SKILL.md → Quick Start
**If you know what you need**: Use Quick Lookup Guide above
**If you want systematic mastery**: Follow Learning Paths in SKILL.md

---

**Meta-navigation**: This file helps you navigate. The library helps you master Mangle.
