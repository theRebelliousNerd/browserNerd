package mcp

import (
	"context"
	"os"
	"testing"
)

// An observation names a control the way assistive technology does.
//
// The page reproduces the controls that were misreported on a live form: text inputs
// whose placeholder is an example value, a radio rendered as a button that a <label>
// names, and a label whose text spans lines. A placeholder or a tooltip is a last resort,
// never a substitute for the label the control is associated with.
const elementLabelHTML = `<!DOCTYPE html>
<html>
<head><title>Element labels</title></head>
<body>
	<form>
		<label for="contact-name">Your name</label>
		<input id="contact-name" placeholder="Jordan Lee">

		<span id="email-caption">Work
			email</span>
		<input id="contact-email" aria-labelledby="email-caption" placeholder="you@company.com">

		<button type="button" role="radio" id="contextualization" aria-checked="true"></button>
		<label for="contextualization">Contextualization only</label>

		<input id="search-only" placeholder="Search deals">
		<button type="button" title="Refresh the graph"><svg aria-hidden="true" width="12" height="12"></svg></button>
	</form>
</body>
</html>`

func TestIntegrationElementLabels(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()
	session, err := sessions.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	page, _ := sessions.Page(session.ID)
	if err := page.Navigate(htmlDataURL(elementLabelHTML)); err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	want := map[string]string{
		"contact-name":      "Your name",
		"contact-email":     "Work email",
		"contextualization": "Contextualization only",
		"search-only":       "Search deals",
	}

	t.Run("interactive elements report the associated label", func(t *testing.T) {
		tool := &GetInteractiveElementsTool{sessions: sessions, engine: engine}
		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":   session.ID,
			"filter":       "all",
			"visible_only": true,
		})
		if err != nil {
			t.Fatalf("GetInteractiveElementsTool failed: %v", err)
		}
		labels := map[string]string{}
		for _, raw := range result.(map[string]interface{})["elements"].([]interface{}) {
			elem := raw.(map[string]interface{})
			label, _ := elem["label"].(string)
			labels[elem["ref"].(string)] = label
		}
		for ref, label := range want {
			if labels[ref] != label {
				t.Errorf("ref %q labelled %q, want %q (all labels: %v)", ref, labels[ref], label, labels)
			}
		}
	})

	t.Run("screenshot overlays report the associated label", func(t *testing.T) {
		boxes, err := (&ScreenshotTool{sessions: sessions, engine: engine}).extractBoundingBoxes(page, false)
		if err != nil {
			t.Fatalf("extractBoundingBoxes failed: %v", err)
		}
		labels := map[string]string{}
		for _, box := range boxes {
			labels[box.Ref] = box.Label
		}
		for ref, label := range want {
			if labels[ref] != label {
				t.Errorf("overlay ref %q labelled %q, want %q (all labels: %v)", ref, labels[ref], label, labels)
			}
		}
		titled := false
		for _, box := range boxes {
			if box.Label == "Refresh the graph" {
				titled = true
			}
		}
		if !titled {
			t.Errorf("a button named only by its title was not reported by that title: %v", labels)
		}
	})
}
