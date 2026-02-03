package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestElementFingerprintSparseJSON verifies omitempty tags work correctly for
// empty strings and empty slices. Note: time.Time zero values are still serialized
// because Go's json package doesn't consider them "empty", but in practice all
// fingerprints have a valid GeneratedAt timestamp.
func TestElementFingerprintSparseJSON(t *testing.T) {
	now := time.Now()

	// Test with only required fields - empty optionals should be omitted
	sparse := ElementFingerprint{
		Ref:         "btn-1",
		TagName:     "button",
		GeneratedAt: now, // Always set in practice
	}

	sparseJSON, err := json.Marshal(sparse)
	if err != nil {
		t.Fatalf("Failed to marshal sparse fingerprint: %v", err)
	}

	// Verify empty string/slice fields are NOT in the JSON
	sparseStr := string(sparseJSON)
	t.Logf("Sparse JSON: %s", sparseStr)

	emptyFieldsToCheck := []string{
		`"id":`,
		`"name":`,
		`"classes":`,
		`"text_content":`,
		`"aria_label":`,
		`"data_testid":`,
		`"role":`,
		`"bounding_box":`,
		`"alt_selectors":`,
	}

	for _, field := range emptyFieldsToCheck {
		if strings.Contains(sparseStr, field) {
			t.Errorf("Sparse JSON should NOT contain empty field %s, got: %s", field, sparseStr)
		}
	}

	// Test with some populated fields
	partial := ElementFingerprint{
		Ref:         "input-1",
		TagName:     "input",
		ID:          "username",
		AriaLabel:   "Enter username",
		GeneratedAt: now,
	}

	partialJSON, err := json.Marshal(partial)
	if err != nil {
		t.Fatalf("Failed to marshal partial fingerprint: %v", err)
	}

	partialStr := string(partialJSON)
	t.Logf("Partial JSON: %s", partialStr)

	// Verify populated fields ARE in the JSON
	if !strings.Contains(partialStr, `"id":"username"`) {
		t.Errorf("Partial JSON should contain id field")
	}
	if !strings.Contains(partialStr, `"aria_label":"Enter username"`) {
		t.Errorf("Partial JSON should contain aria_label field")
	}

	// Verify empty fields are NOT in the JSON
	if strings.Contains(partialStr, `"name":`) {
		t.Errorf("Partial JSON should NOT contain empty name field")
	}
	if strings.Contains(partialStr, `"classes":`) {
		t.Errorf("Partial JSON should NOT contain empty classes field")
	}
	if strings.Contains(partialStr, `"data_testid":`) {
		t.Errorf("Partial JSON should NOT contain empty data_testid field")
	}
	if strings.Contains(partialStr, `"role":`) {
		t.Errorf("Partial JSON should NOT contain empty role field")
	}
	if strings.Contains(partialStr, `"alt_selectors":`) {
		t.Errorf("Partial JSON should NOT contain empty alt_selectors field")
	}

	// Log sizes to show token savings
	t.Logf("Sparse JSON length: %d bytes", len(sparseJSON))
	t.Logf("Partial JSON length: %d bytes", len(partialJSON))

	// Compare against what it would be without omitempty
	// A fully populated fingerprint with empty values would be much larger
	fullWithEmptyValues := `{"ref":"btn-1","tag_name":"button","id":"","name":"","classes":[],"text_content":"","aria_label":"","data_testid":"","role":"","bounding_box":{},"alt_selectors":[],"generated_at":"` + now.Format(time.RFC3339Nano) + `"}`
	t.Logf("Would be without omitempty: ~%d bytes (with empty values)", len(fullWithEmptyValues))
	t.Logf("Token savings: ~%d bytes per fingerprint", len(fullWithEmptyValues)-len(sparseJSON))
}
