package mcp

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
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
			if fmt.Sprintf("%v", wantArgs[i]) == "_" {
				continue
			}
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
// 1. Prefixed refs (testid:X, aria:X, row:X, rowkey:X) - parsed and used directly
// 2. data-testid attribute from fingerprint
// 3. aria-label attribute from fingerprint
// 4. ID attribute (from ref or fingerprint)
// 5. name attribute (from ref or fingerprint)
// 6. Original ref as CSS selector
func findElementByRefWithRegistry(page *rod.Page, ref string, registry *browser.ElementRegistry) (*rod.Element, error) {
	timeout := 2 * time.Second
	baseRef, duplicateIndex, hasDuplicateSuffix := splitDuplicateSuffix(ref)

	// Strategy 1: Handle prefixed refs (testid:X, aria:X, row:X)
	if strings.HasPrefix(baseRef, "testid:") {
		testID := strings.TrimPrefix(baseRef, "testid:")
		selectors := []string{
			`[data-testid="` + escapeAttributeValue(testID) + `"]`,
			`[data-test-id="` + escapeAttributeValue(testID) + `"]`,
		}
		for _, sel := range selectors {
			if el, ok := findBySelectorWithFingerprint(page, timeout, sel, nil, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}
	}

	if strings.HasPrefix(baseRef, "aria:") {
		// Reconstruct aria-label from sanitized ref
		ariaRef := strings.TrimPrefix(baseRef, "aria:")
		// We need to iterate since aria-label was sanitized during ref generation
		elements, err := page.Timeout(timeout).Elements(`[aria-label]`)
		if err == nil {
			for i, elem := range elements {
				if hasDuplicateSuffix && i != duplicateIndex {
					continue
				}
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

	if strings.HasPrefix(baseRef, "row:") {
		rowCandidates := []string{decodeRefPart(strings.TrimPrefix(baseRef, "row:"))}
		if baseRef != ref && strings.HasPrefix(ref, "row:") {
			rowCandidates = append(rowCandidates, decodeRefPart(strings.TrimPrefix(ref, "row:")))
		}
		for _, rowID := range rowCandidates {
			selectors := buildRowIndexSelectors(rowID)
			for _, sel := range selectors {
				if el, ok := findBySelectorWithFingerprint(page, timeout, sel, nil, duplicateIndex, hasDuplicateSuffix); ok {
					return el, nil
				}
			}
		}
	}

	if strings.HasPrefix(baseRef, "rowkey:") {
		rowKeyCandidates := []string{decodeRefPart(strings.TrimPrefix(baseRef, "rowkey:"))}
		if baseRef != ref && strings.HasPrefix(ref, "rowkey:") {
			rowKeyCandidates = append(rowKeyCandidates, decodeRefPart(strings.TrimPrefix(ref, "rowkey:")))
		}
		for _, rowKey := range rowKeyCandidates {
			selectors := buildRowKeySelectors(rowKey)
			for _, sel := range selectors {
				if el, ok := findBySelectorWithFingerprint(page, timeout, sel, nil, duplicateIndex, hasDuplicateSuffix); ok {
					return el, nil
				}
			}
		}
	}

	// Strategy 2-5: Use fingerprint data if available from registry
	var fp *browser.ElementFingerprint
	if registry != nil {
		fp = registry.Get(ref)
		if fp == nil && baseRef != ref {
			fp = registry.Get(baseRef)
		}
	}

	if fp != nil {
		// Try data-testid from fingerprint
		if fp.DataTestID != "" {
			selectors := []string{
				`[data-testid="` + escapeAttributeValue(fp.DataTestID) + `"]`,
				`[data-test-id="` + escapeAttributeValue(fp.DataTestID) + `"]`,
			}
			for _, sel := range selectors {
				if el, ok := findBySelectorWithFingerprint(page, timeout, sel, fp, duplicateIndex, hasDuplicateSuffix); ok {
					return el, nil
				}
			}
		}

		// Try aria-label from fingerprint
		if fp.AriaLabel != "" {
			if el, ok := findBySelectorWithFingerprint(page, timeout, `[aria-label="`+escapeAttributeValue(fp.AriaLabel)+`"]`, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}

		// Try ID from fingerprint
		if fp.ID != "" {
			if el, ok := findBySelectorWithFingerprint(page, timeout, "#"+escapeCSSSelector(fp.ID), fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}

		// Try name from fingerprint
		if fp.Name != "" {
			if el, ok := findBySelectorWithFingerprint(page, timeout, `[name="`+escapeAttributeValue(fp.Name)+`"]`, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}

		// Try tag + classes from fingerprint (supports utility classes with special chars)
		if selector := buildSelectorFromFingerprintClasses(fp); selector != "" {
			if el, ok := findBySelectorWithFingerprint(page, timeout, selector, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}

		if fp.Role != "" {
			roleSelector := `[role="` + escapeAttributeValue(fp.Role) + `"]`
			if fp.TagName != "" {
				roleSelector = fp.TagName + roleSelector
			}
			if el, ok := findBySelectorWithFingerprint(page, timeout, roleSelector, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}

		if fp.RowIndex != "" {
			for _, sel := range buildRowIndexSelectors(fp.RowIndex) {
				if el, ok := findBySelectorWithFingerprint(page, timeout, sel, fp, duplicateIndex, hasDuplicateSuffix); ok {
					return el, nil
				}
			}
		}

		if fp.RowKey != "" {
			for _, sel := range buildRowKeySelectors(fp.RowKey) {
				if el, ok := findBySelectorWithFingerprint(page, timeout, sel, fp, duplicateIndex, hasDuplicateSuffix); ok {
					return el, nil
				}
			}
		}
	}

	// Strategy 6: Fallback to original ref-based search
	tryByIDOrName := func(candidate string) (*rod.Element, bool) {
		if candidate == "" {
			return nil, false
		}
		if el, ok := findBySelectorWithFingerprint(page, timeout, "#"+escapeCSSSelector(candidate), fp, duplicateIndex, hasDuplicateSuffix); ok {
			return el, true
		}
		if el, ok := findBySelectorWithFingerprint(page, timeout, `[name="`+escapeAttributeValue(candidate)+`"]`, fp, duplicateIndex, hasDuplicateSuffix); ok {
			return el, true
		}
		return nil, false
	}

	if el, ok := tryByIDOrName(ref); ok {
		return el, nil
	}
	if baseRef != ref {
		if el, ok := tryByIDOrName(baseRef); ok {
			return el, nil
		}
	}

	// Try as raw CSS selector first if it looks like a valid tag.class pattern
	// Refs like "button.inline-flex.items-center" are already valid CSS selectors
	if looksLikeCSSSelector(ref) {
		if el, ok := findBySelectorWithFingerprint(page, timeout, ref, fp, duplicateIndex, hasDuplicateSuffix); ok {
			return el, nil
		}
	}
	if baseRef != ref && looksLikeCSSSelector(baseRef) {
		if el, ok := findBySelectorWithFingerprint(page, timeout, baseRef, fp, duplicateIndex, hasDuplicateSuffix); ok {
			return el, nil
		}
	}

	// Try a parsed/escaped class selector for refs with utility syntax:
	// e.g. "div.group-hover:bg-slate-50.data-[state=open]:opacity-100"
	if selector, dedupeIdx, hasDedupe, ok := buildEscapedClassSelector(ref); ok {
		if el, ok := findBySelectorWithFingerprint(page, timeout, selector, fp, dedupeIdx, hasDedupe); ok {
			return el, nil
		}
	} else if strings.Contains(baseRef, ".") {
		if classSelector := escapeClassSegments(baseRef); classSelector != "" {
			if el, ok := findBySelectorWithFingerprint(page, timeout, classSelector, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}
	}

	// Refs like "button[14]" are emitted when no better identifier exists.
	if tag, idx, ok := parseIndexedTagRef(ref); ok {
		elements, err := page.Timeout(timeout).Elements(tag)
		if err == nil && len(elements) > 0 {
			if idx >= 0 && idx < len(elements) {
				return elements[idx], nil
			}
			return elements[len(elements)-1], nil
		}
	}

	// Try as escaped CSS selector (for refs that need escaping like IDs with special chars)
	escapedRef := escapeCSSSelector(ref)
	if escapedRef != ref { // Only try if escaping changed something
		if el, ok := findBySelectorWithFingerprint(page, timeout, escapedRef, fp, duplicateIndex, hasDuplicateSuffix); ok {
			return el, nil
		}
	}

	// Strategy 7: Try alt_selectors from fingerprint as last resort
	// These are pre-computed CSS selectors that were captured during element discovery
	if fp != nil && len(fp.AltSelectors) > 0 {
		for _, altSel := range fp.AltSelectors {
			if el, ok := findBySelectorWithFingerprint(page, timeout, altSel, fp, duplicateIndex, hasDuplicateSuffix); ok {
				return el, nil
			}
		}
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

func decodeRefPart(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func buildRowIndexSelectors(rowIndex string) []string {
	if strings.TrimSpace(rowIndex) == "" {
		return nil
	}
	escaped := escapeAttributeValue(rowIndex)
	return []string{
		`[role="row"][aria-rowindex="` + escaped + `"]`,
		`[role="row"][data-rowindex="` + escaped + `"]`,
		`[role="row"][row-index="` + escaped + `"]`,
		`[role="row"][data-row-index="` + escaped + `"]`,
		`tr[aria-rowindex="` + escaped + `"]`,
		`tr[data-rowindex="` + escaped + `"]`,
		`tr[row-index="` + escaped + `"]`,
		`tr[data-row-index="` + escaped + `"]`,
		`[aria-rowindex="` + escaped + `"]`,
		`[data-rowindex="` + escaped + `"]`,
		`[row-index="` + escaped + `"]`,
		`[data-row-index="` + escaped + `"]`,
	}
}

func buildRowKeySelectors(rowKey string) []string {
	if strings.TrimSpace(rowKey) == "" {
		return nil
	}
	escaped := escapeAttributeValue(rowKey)
	attrs := []string{
		"data-row-id",
		"data-row-key",
		"data-id",
		"data-key",
		"data-item-id",
		"data-uid",
	}
	selectors := make([]string, 0, len(attrs)*3)
	for _, attr := range attrs {
		selectors = append(selectors,
			`[role="row"][`+attr+`="`+escaped+`"]`,
			`tr[`+attr+`="`+escaped+`"]`,
			`[`+attr+`="`+escaped+`"]`,
		)
	}
	return selectors
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

	// Check role consistency (warning-level)
	if fp.Role != "" {
		actualRole, err := element.Attribute("role")
		actual := ""
		if err == nil && actualRole != nil {
			actual = *actualRole
		}
		if actual != "" && actual != fp.Role {
			result.Changes = append(result.Changes, fmt.Sprintf("role: expected %s, got %s", fp.Role, actual))
			result.Score -= 0.1
		}
	}

	// Check class overlap (useful when refs are class-derived in utility-heavy UIs)
	if len(fp.Classes) > 0 {
		actualClassAttr, err := element.Attribute("class")
		if err == nil {
			classSet := make(map[string]bool)
			if actualClassAttr != nil {
				for _, cls := range strings.Fields(*actualClassAttr) {
					classSet[cls] = true
				}
			}

			matches := 0
			for _, cls := range fp.Classes {
				if classSet[cls] {
					matches++
				}
			}

			if matches == 0 {
				result.Changes = append(result.Changes, "classes: changed")
				result.Score -= 0.2
			} else if matches < len(fp.Classes) {
				result.Score -= 0.05
			}
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

func escapeClassSegments(ref string) string {
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, ".")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = escapeCSSSelector(part)
	}
	return strings.Join(parts, ".")
}

// looksLikeCSSSelector checks if a string looks like a valid CSS tag.class selector.
// Returns true for patterns like "button.class1.class2" that shouldn't be escaped.
// These are generated by get-interactive-elements when no stable identifier exists.
func looksLikeCSSSelector(s string) bool {
	// Must contain a dot to be a class-based selector
	if !strings.Contains(s, ".") {
		return false
	}

	// Split into segments
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}

	// First part should be a valid HTML tag or empty (for .class selectors)
	firstPart := parts[0]
	if firstPart != "" {
		// Check if it's a reasonable HTML tag (lowercase, alphanumeric)
		for _, r := range firstPart {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}

	// Remaining parts should be valid CSS class names (alphanumeric, hyphen, underscore)
	for _, part := range parts[1:] {
		if part == "" {
			return false // Empty class name not valid
		}
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				return false
			}
		}
	}

	return true
}

func splitDuplicateSuffix(ref string) (string, int, bool) {
	idx := strings.LastIndex(ref, "_")
	if idx <= 0 || idx >= len(ref)-1 {
		return ref, 0, false
	}

	base := ref[:idx]
	if !strings.Contains(base, ".") && !strings.Contains(base, "[") && !strings.HasPrefix(base, "row:") && !strings.HasPrefix(base, "rowkey:") {
		return ref, 0, false
	}

	n, err := strconv.Atoi(ref[idx+1:])
	if err != nil || n < 0 {
		return ref, 0, false
	}

	return base, n, true
}

func isSimpleTagToken(tag string) bool {
	if tag == "" {
		return false
	}
	for _, r := range tag {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

func buildEscapedClassSelector(ref string) (string, int, bool, bool) {
	baseRef, duplicateIndex, hasDuplicate := splitDuplicateSuffix(ref)
	if !strings.Contains(baseRef, ".") {
		return "", 0, false, false
	}

	parts := strings.Split(baseRef, ".")
	if len(parts) < 2 {
		return "", 0, false, false
	}

	tag := strings.TrimSpace(parts[0])
	classParts := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", 0, false, false
		}
		classParts = append(classParts, escapeCSSSelector(part))
	}
	if len(classParts) == 0 {
		return "", 0, false, false
	}

	selector := "." + strings.Join(classParts, ".")
	if tag != "" {
		if !isSimpleTagToken(tag) {
			return "", 0, false, false
		}
		selector = tag + selector
	}

	return selector, duplicateIndex, hasDuplicate, true
}

func parseIndexedTagRef(ref string) (string, int, bool) {
	baseRef, _, _ := splitDuplicateSuffix(ref)
	openIdx := strings.LastIndex(baseRef, "[")
	closeIdx := strings.LastIndex(baseRef, "]")
	if openIdx <= 0 || closeIdx != len(baseRef)-1 || openIdx+1 >= closeIdx {
		return "", 0, false
	}

	tag := strings.TrimSpace(baseRef[:openIdx])
	if !isSimpleTagToken(tag) {
		return "", 0, false
	}

	n, err := strconv.Atoi(baseRef[openIdx+1 : closeIdx])
	if err != nil || n < 0 {
		return "", 0, false
	}

	return tag, n, true
}

func buildSelectorFromFingerprintClasses(fp *browser.ElementFingerprint) string {
	if fp == nil || fp.TagName == "" || len(fp.Classes) == 0 {
		return ""
	}

	classes := make([]string, 0, len(fp.Classes))
	for i, cls := range fp.Classes {
		if i >= 3 {
			break
		}
		cls = strings.TrimSpace(cls)
		if cls == "" {
			continue
		}
		classes = append(classes, escapeCSSSelector(cls))
	}
	if len(classes) == 0 {
		return ""
	}

	return fp.TagName + "." + strings.Join(classes, ".")
}

func selectBestElementCandidate(elements []*rod.Element, fp *browser.ElementFingerprint, duplicateIndex int, hasDuplicateSuffix bool) *rod.Element {
	if len(elements) == 0 {
		return nil
	}

	if hasDuplicateSuffix && duplicateIndex >= 0 && duplicateIndex < len(elements) {
		return elements[duplicateIndex]
	}

	if fp == nil || len(elements) == 1 {
		return elements[0]
	}

	best := elements[0]
	bestScore := -1.0
	for _, element := range elements {
		score := validateFingerprint(element, fp).Score
		if score > bestScore {
			best = element
			bestScore = score
		}
	}

	return best
}

func findBySelectorWithFingerprint(page *rod.Page, timeout time.Duration, selector string, fp *browser.ElementFingerprint, duplicateIndex int, hasDuplicateSuffix bool) (*rod.Element, bool) {
	elements, err := page.Timeout(timeout).Elements(selector)
	if err != nil || len(elements) == 0 {
		return nil, false
	}

	chosen := selectBestElementCandidate(elements, fp, duplicateIndex, hasDuplicateSuffix)
	if chosen == nil {
		return nil, false
	}

	return chosen, true
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
