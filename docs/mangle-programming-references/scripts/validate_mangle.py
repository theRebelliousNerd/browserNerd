#!/usr/bin/env python3
"""
Validate Mangle program syntax and structure.

Usage:
    python3 validate_mangle.py <path_to_mangle_file>
"""

import sys
import re
from pathlib import Path

def validate_mangle_file(filepath):
    """Validate basic Mangle syntax."""
    with open(filepath) as f:
        content = f.read()
    
    errors = []
    warnings = []
    
    # Check for common syntax errors
    lines = content.split('\n')
    for i, line in enumerate(lines, 1):
        line = line.strip()
        
        # Skip comments and empty lines
        if not line or line.startswith('#'):
            continue
        
        # Check for missing periods on facts/rules
        if line and not line.endswith('.') and not line.startswith('?'):
            if ':-' in line or any(c.isupper() for c in line):
                errors.append(f"Line {i}: Missing period at end of fact/rule")
        
        # Check for lowercase variables (common mistake)
        if ':-' in line:
            # Extract variables (simple heuristic)
            parts = line.split(':-')
            if len(parts) == 2:
                body = parts[1]
                vars = re.findall(r'\b([a-z]+)\b', body)
                if vars and not all(v in ['not', 'do', 'let']):
                    warnings.append(f"Line {i}: Possible lowercase variable: {vars}")
    
    # Report results
    print(f"Validating: {filepath}")
    print(f"Lines: {len(lines)}")
    
    if errors:
        print("\n❌ ERRORS:")
        for error in errors:
            print(f"  {error}")
    
    if warnings:
        print("\n⚠️  WARNINGS:")
        for warning in warnings:
            print(f"  {warning}")
    
    if not errors and not warnings:
        print("\n✅ No issues found!")
    
    return len(errors) == 0

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(1)
    
    filepath = Path(sys.argv[1])
    if not filepath.exists():
        print(f"Error: File not found: {filepath}")
        sys.exit(1)
    
    success = validate_mangle_file(filepath)
    sys.exit(0 if success else 1)
