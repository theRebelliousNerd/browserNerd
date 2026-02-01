#!/usr/bin/env python3
"""
Generate Mangle program templates for common use cases.

Usage:
    python3 generate_template.py <template_type> <output_file>

Available templates:
    - vulnerability_scanner
    - graph_analysis
    - policy_checker
    - basic_rules
"""

import sys
from pathlib import Path

TEMPLATES = {
    "vulnerability_scanner": """# ========================================
# SOFTWARE VULNERABILITY SCANNER
# ========================================

# === PROJECT DEFINITIONS ===
project(/my_project, "MyProject", "1.0.0").

# === DEPENDENCIES ===
# Add your dependencies here
# depends_on(/my_project, /library_name, "version").

# === VULNERABILITY DATABASE ===
# Add known vulnerabilities here
# cve_affects("library_name", "version", "CVE-XXXX-XXXXX", /severity).

# === ANALYSIS RULES ===

# Transitive dependencies
has_dependency(Project, Lib, Version) :- 
    depends_on(Project, Lib, Version).

has_dependency(Project, Lib, Version) :- 
    depends_on(Project, Intermediate, _),
    has_dependency(Intermediate, Lib, Version).

# Detect vulnerabilities
vulnerable_project(Project, CVE, Severity) :- 
    project(Project, _, _),
    has_dependency(Project, Lib, Version),
    cve_affects(Lib, Version, CVE, Severity).

# High-risk projects
high_risk_project(Project) :- 
    vulnerable_project(Project, _, /critical).

# === QUERIES ===
# ?vulnerable_project(P, CVE, Sev)
# ?high_risk_project(P)
""",

    "graph_analysis": """# ========================================
# GRAPH REACHABILITY ANALYSIS
# ========================================

# === GRAPH EDGES ===
# Add your edges here
# edge(/node1, /node2).

# === ANALYSIS RULES ===

# Transitive closure (reachability)
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- edge(X, Y), reachable(Y, Z).

# Path length
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

# Cycle detection
cycle_edge(X, Y) :- 
    edge(X, Y),
    reachable(Y, X).

# === QUERIES ===
# ?reachable(Start, End)
# ?shortest_path(Start, End, Len)
# ?cycle_edge(X, Y)
""",

    "policy_checker": """# ========================================
# INFRASTRUCTURE POLICY CHECKER
# ========================================

# === INFRASTRUCTURE STATE ===
# Add your resources here
# server(/server_id, /region, /environment, /status).

# === POLICY DEFINITIONS ===
# Add your policies here
# approved_region(/us_east).

# === COMPLIANCE RULES ===

# Policy violation detection
policy_violation(Resource, PolicyType) :- 
    # Add your policy logic here
    false.  # Replace with actual logic

# Compliance summary
compliance_summary(PolicyType, Count) :- 
    policy_violation(_, PolicyType) |> 
    do fn:group_by(PolicyType), 
    let Count = fn:Count().

# === QUERIES ===
# ?policy_violation(R, Type)
# ?compliance_summary(Type, Count)
""",

    "basic_rules": """# ========================================
# BASIC MANGLE PROGRAM
# ========================================

# === FACTS ===
# Add your facts here
# Examples:
# person(/alice).
# person(/bob).

# === RULES ===
# Add your rules here
# Examples:
# sibling(X, Y) :- parent(P, X), parent(P, Y), X != Y.

# === QUERIES ===
# Add your queries here
# Examples:
# ?person(X)
""",
}

def generate_template(template_type, output_file):
    """Generate a Mangle template."""
    if template_type not in TEMPLATES:
        print(f"❌ Unknown template type: {template_type}")
        print(f"\nAvailable templates:")
        for name in TEMPLATES.keys():
            print(f"  - {name}")
        return False
    
    content = TEMPLATES[template_type]
    
    output_path = Path(output_file)
    output_path.write_text(content)
    
    print(f"✅ Generated {template_type} template: {output_file}")
    return True

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(__doc__)
        sys.exit(1)
    
    template_type = sys.argv[1]
    output_file = sys.argv[2]
    
    success = generate_template(template_type, output_file)
    sys.exit(0 if success else 1)
