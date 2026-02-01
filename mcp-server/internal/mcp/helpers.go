package mcp

import (
	"fmt"
	"math"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
)

func matchFact(facts []mangle.Fact, wantArgs []interface{}) bool {
	if len(wantArgs) == 0 {
		return len(facts) > 0
	}
	for _, f := range facts {
		if len(f.Args) < len(wantArgs) {
			continue
		}
		ok := true
		for i := range wantArgs {
			if fmt.Sprintf("%v", f.Args[i]) != fmt.Sprintf("%v", wantArgs[i]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// findElementByRef finds an element by ref, handling CSS selector escaping for special characters.
// Tries in order: ID, name attribute, escaped CSS selector.
// This is the legacy version without registry support - use findElementByRefWithRegistry for better reliability.
func findElementByRef(page *rod.Page, ref string) (*rod.Element, error) {
	return findElementByRefWithRegistry(page, ref, nil)
}

// findElementByRefWithRegistry finds an element using multi-strategy search with fingerprint support.
// Search order prioritizes stable identifiers:
// 1. Prefixed refs (testid:X, aria:X) - parsed and used directly
// 2. data-testid attribute from fingerprint
// 3. aria-label attribute from fingerprint
// 4. ID attribute (from ref or fingerprint)
// 5. name attribute (from ref or fingerprint)
// 6. Original ref as CSS selector
func findElementByRefWithRegistry(page *rod.Page, ref string, registry *browser.ElementRegistry) (*rod.Element, error) {
	timeout := 2 * time.Second

	// Strategy 1: Handle prefixed refs (testid:X or aria:X)
	if strings.HasPrefix(ref, "testid:") {
		testID := strings.TrimPrefix(ref, "testid:")
		el, err := page.Timeout(timeout).Element(`[data-testid="` + testID + `"]`)
		if err == nil {
			return el, nil
		}
		// Also try data-test-id variant
		el, err = page.Timeout(timeout).Element(`[data-test-id="` + testID + `"]`)
		if err == nil {
			return el, nil
		}
	}

	if strings.HasPrefix(ref, "aria:") {
		// Reconstruct aria-label from sanitized ref
		ariaRef := strings.TrimPrefix(ref, "aria:")
		// We need to iterate since aria-label was sanitized during ref generation
		elements, err := page.Timeout(timeout).Elements(`[aria-label]`)
		if err == nil {
			for _, elem := range elements {
				label, _ := elem.Attribute("aria-label")
				if label != nil {
					// Check if sanitized version matches
					sanitized := sanitizeAriaLabel(*label)
					if sanitized == ariaRef || strings.HasPrefix(sanitized, ariaRef) {
						return elem, nil
					}
				}
			}
		}
	}

	// Strategy 2-5: Use fingerprint data if available from registry
	var fp *browser.ElementFingerprint
	if registry != nil {
		fp = registry.Get(ref)
	}

	if fp != nil {
		// Try data-testid from fingerprint
		if fp.DataTestID != "" {
			el, err := page.Timeout(timeout).Element(`[data-testid="` + fp.DataTestID + `"]`)
			if err == nil {
				return el, nil
			}
		}

		// Try aria-label from fingerprint
		if fp.AriaLabel != "" {
			el, err := page.Timeout(timeout).Element(`[aria-label="` + escapeAttributeValue(fp.AriaLabel) + `"]`)
			if err == nil {
				return el, nil
			}
		}

		// Try ID from fingerprint
		if fp.ID != "" {
			el, err := page.Timeout(timeout).Element("#" + escapeCSSSelector(fp.ID))
			if err == nil {
				return el, nil
			}
		}

		// Try name from fingerprint
		if fp.Name != "" {
			el, err := page.Timeout(timeout).Element(`[name="` + fp.Name + `"]`)
			if err == nil {
				return el, nil
			}
		}
	}

	// Strategy 6: Fallback to original ref-based search
	// Try by ID (ref might be an element ID)
	el, err := page.Timeout(timeout).Element("#" + escapeCSSSelector(ref))
	if err == nil {
		return el, nil
	}

	// Try by name attribute
	el, err = page.Timeout(timeout).Element(`[name="` + ref + `"]`)
	if err == nil {
		return el, nil
	}

	// Try as escaped CSS selector
	escapedRef := escapeCSSSelector(ref)
	el, err = page.Timeout(timeout).Element(escapedRef)
	if err == nil {
		return el, nil
	}

	// Build informative error message
	if fp != nil {
		return nil, fmt.Errorf("element not found: %s (fingerprint: tag=%s, id=%s, testid=%s)", ref, fp.TagName, fp.ID, fp.DataTestID)
	}
	return nil, fmt.Errorf("element not found: %s (no fingerprint in registry)", ref)
}

// sanitizeAriaLabel converts aria-label to the same format used in ref generation
func sanitizeAriaLabel(label string) string {
	var result []rune
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result = append(result, r)
		} else {
			result = append(result, '_')
		}
	}
	s := string(result)
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// escapeAttributeValue escapes characters for use in CSS attribute selectors
func escapeAttributeValue(s string) string {
	// Replace quotes and backslashes
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// FingerprintValidationResult contains the result of validating an element against its fingerprint
type FingerprintValidationResult struct {
	Valid   bool     // Whether the element matches the fingerprint
	Changes []string // List of what changed (for warnings)
	Score   float64  // 0.0 to 1.0 similarity score
}

// validateFingerprint checks if a found element still matches its stored fingerprint.
// Returns validation result with changes detected and similarity score.
func validateFingerprint(element *rod.Element, fp *browser.ElementFingerprint) FingerprintValidationResult {
	if fp == nil {
		return FingerprintValidationResult{Valid: true, Score: 1.0}
	}

	result := FingerprintValidationResult{
		Valid:   true,
		Changes: make([]string, 0),
		Score:   1.0,
	}

	// Check tag name (critical - must match)
	tagName, err := element.Property("tagName")
	if err == nil {
		actualTag := strings.ToLower(tagName.Str())
		if fp.TagName != "" && actualTag != fp.TagName {
			result.Valid = false
			result.Changes = append(result.Changes, fmt.Sprintf("tag_name: expected %s, got %s", fp.TagName, actualTag))
			result.Score -= 0.3
		}
	}

	// Check text content similarity (warning - content can change)
	if fp.TextContent != "" {
		text, err := element.Text()
		if err == nil {
			text = strings.TrimSpace(text)
			if len(text) > 100 {
				text = text[:100]
			}
			// Fuzzy match - check if first 50 chars are similar
			fpPrefix := fp.TextContent
			if len(fpPrefix) > 50 {
				fpPrefix = fpPrefix[:50]
			}
			textPrefix := text
			if len(textPrefix) > 50 {
				textPrefix = textPrefix[:50]
			}
			if fpPrefix != "" && !strings.Contains(text, fpPrefix) && !strings.Contains(fpPrefix, textPrefix) {
				result.Changes = append(result.Changes, "text_content: changed")
				result.Score -= 0.1
			}
		}
	}

	// Check bounding box position (warning - element may have moved)
	if fp.BoundingBox != nil && len(fp.BoundingBox) > 0 {
		shape, err := element.Shape()
		if err == nil && shape != nil {
			box := shape.Box()
			if box != nil {
				fpX, hasX := fp.BoundingBox["x"]
				fpY, hasY := fp.BoundingBox["y"]
				if hasX && hasY {
					dx := math.Abs(box.X - fpX)
					dy := math.Abs(box.Y - fpY)
					// Allow up to 100px movement before warning
					if dx > 100 || dy > 100 {
						result.Changes = append(result.Changes, fmt.Sprintf("position: moved by (%.0f, %.0f)px", dx, dy))
						result.Score -= 0.1
					}
				}
			}
		}
	}

	// Check ID consistency
	if fp.ID != "" {
		actualID, err := element.Attribute("id")
		if err == nil && actualID != nil && *actualID != fp.ID {
			result.Changes = append(result.Changes, fmt.Sprintf("id: expected %s, got %s", fp.ID, *actualID))
			result.Score -= 0.2
		}
	}

	// Ensure score doesn't go negative
	if result.Score < 0 {
		result.Score = 0
	}

	return result
}

// escapeCSSSelector escapes special characters in CSS selectors.
// Characters that need escaping: / . : [ ] ( ) # > + ~ = ^ $ * | ! @ % & ' " ` { }
func escapeCSSSelector(s string) string {
	var result []rune
	for _, r := range s {
		switch r {
		case '/', '.', ':', '[', ']', '(', ')', '#', '>', '+', '~', '=', '^', '$', '*', '|', '!', '@', '%', '&', '\'', '"', '`', '{', '}', ' ':
			// Escape with backslash for CSS
			result = append(result, '\\', r)
		default:
			result = append(result, r)
		}
	}
	return string(result)
}

func getStringArg(args map[string]interface{}, key string) string {
	return getStringFromMap(args, key)
}

func getStringFromMap(args map[string]interface{}, key string) string {
	val, ok := args[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getIntArg(args map[string]interface{}, key string, fallback int) int {
	val, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return fallback
	}
}

// getBoolArg extracts a boolean argument with default.
func getBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	val, ok := args[key]
	if !ok {
		return fallback
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return fallback
}

// classifyJSError categorizes JavaScript execution errors for better debugging.
func classifyJSError(err error) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()

	// Check for timeout errors
	if strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "Timeout") {
		return "timeout"
	}

	// Check for syntax errors
	if strings.Contains(errStr, "SyntaxError") ||
		strings.Contains(errStr, "Unexpected token") ||
		strings.Contains(errStr, "Unexpected identifier") {
		return "syntax"
	}

	// Check for reference/type errors (runtime)
	if strings.Contains(errStr, "ReferenceError") ||
		strings.Contains(errStr, "TypeError") ||
		strings.Contains(errStr, "is not defined") ||
		strings.Contains(errStr, "is not a function") ||
		strings.Contains(errStr, "Cannot read property") ||
		strings.Contains(errStr, "Cannot read properties") {
		return "runtime"
	}

	// Check for promise/async errors
	if strings.Contains(errStr, "Promise") ||
		strings.Contains(errStr, "async") ||
		strings.Contains(errStr, "await") {
		return "async"
	}

	// Check for security errors
	if strings.Contains(errStr, "SecurityError") ||
		strings.Contains(errStr, "cross-origin") ||
		strings.Contains(errStr, "blocked") {
		return "security"
	}

	return "unknown"
}

// formatJSError formats a JavaScript error for human-readable output.
func formatJSError(err error) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()

	// Try to extract the actual JavaScript error message from CDP wrapper
	// CDP errors often look like: "runtime error: ReferenceError: foo is not defined"
	if strings.Contains(errStr, "ReferenceError:") {
		parts := strings.SplitN(errStr, "ReferenceError:", 2)
		if len(parts) == 2 {
			return "ReferenceError:" + strings.TrimSpace(parts[1])
		}
	}
	if strings.Contains(errStr, "TypeError:") {
		parts := strings.SplitN(errStr, "TypeError:", 2)
		if len(parts) == 2 {
			return "TypeError:" + strings.TrimSpace(parts[1])
		}
	}
	if strings.Contains(errStr, "SyntaxError:") {
		parts := strings.SplitN(errStr, "SyntaxError:", 2)
		if len(parts) == 2 {
			return "SyntaxError:" + strings.TrimSpace(parts[1])
		}
	}

	// For timeout errors, provide clear message
	if strings.Contains(errStr, "context deadline exceeded") {
		return "Script execution timed out"
	}

	// Truncate very long errors
	if len(errStr) > 200 {
		return errStr[:197] + "..."
	}

	return errStr
}

