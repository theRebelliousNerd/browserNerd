package mcp

import "fmt"

// The label describes the bound actually applied, not the order in which
// the caller supplied narrowing options.
func reasonScopeLabel(effectiveSinceMs, actionSinceMs, navigationSinceMs int64, timeWindowMs int) string {
	switch {
	case effectiveSinceMs > 0 && effectiveSinceMs == actionSinceMs:
		return "since your last action"
	case effectiveSinceMs > 0 && effectiveSinceMs == navigationSinceMs:
		return "since the last navigation"
	case timeWindowMs > 0:
		if timeWindowMs%1000 != 0 {
			return fmt.Sprintf("in the last %dms of session history", timeWindowMs)
		}
		return fmt.Sprintf("in the last %ds of session history", timeWindowMs/1000)
	default:
		return "in all retained session history"
	}
}

func scopedIssueVerdict(scope, issue string) string {
	return scope + ": " + issue
}
