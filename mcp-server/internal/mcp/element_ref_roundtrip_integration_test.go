package mcp

import (
	"context"
	"os"
	"testing"
)

// A ref handed out by any observation must resolve to the element it labelled.
//
// The page reproduces the shape that made clicks land on the wrong control: several
// buttons share their first classes, the first carries an aria-label, one is hidden,
// one is disabled, and the targets ("30d", "90d") carry no stable identifier at all.
// Resolving a positional suffix against a different element population clicked the
// sidebar toggle and the disabled Search button instead of the window presets.
const refRoundTripHTML = `<!DOCTYPE html>
<html>
<head><title>Ref round trip</title></head>
<body>
	<nav>
		<button class="inline-flex items-center justify-center" aria-label="Collapse sidebar">|</button>
		<button class="inline-flex items-center justify-center" style="display:none">Hidden menu</button>
		<button class="inline-flex items-center justify-center" disabled>Search</button>
		<div role="tab" class="inline-flex items-center justify-center">Tab</div>
	</nav>
	<a class="inline-flex items-center" href="#elsewhere">Link sharing classes</a>
	<fieldset>
		<button class="inline-flex items-center justify-center">30d</button>
		<button class="inline-flex items-center justify-center">90d</button>
	</fieldset>
	<button class="inline-flex items-center justify-center" data-testid="save">Save</button>
</body>
</html>`

func TestIntegrationElementRefsRoundTrip(t *testing.T) {
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
	if err := page.Navigate(htmlDataURL(refRoundTripHTML)); err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	interactiveRefs := func(t *testing.T, filter string) map[string]string {
		t.Helper()
		tool := &GetInteractiveElementsTool{sessions: sessions, engine: engine}
		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":   session.ID,
			"filter":       filter,
			"visible_only": true,
		})
		if err != nil {
			t.Fatalf("GetInteractiveElementsTool(%s) failed: %v", filter, err)
		}
		refs := map[string]string{}
		for _, raw := range result.(map[string]interface{})["elements"].([]interface{}) {
			elem := raw.(map[string]interface{})
			if label, ok := elem["label"].(string); ok {
				refs[label] = elem["ref"].(string)
			}
		}
		return refs
	}

	resolvedText := func(t *testing.T, ref string) string {
		t.Helper()
		element, err := findElementByRefWithRegistry(page, ref, sessions.Registry(session.ID))
		if err != nil {
			t.Fatalf("ref %q did not resolve: %v", ref, err)
		}
		text, err := element.Text()
		if err != nil {
			t.Fatalf("ref %q resolved to an unreadable element: %v", ref, err)
		}
		return text
	}

	for _, filter := range []string{"buttons", "all"} {
		t.Run("interactive refs resolve to their own element, filter="+filter, func(t *testing.T) {
			refs := interactiveRefs(t, filter)
			for _, label := range []string{"30d", "90d", "Search", "Save"} {
				ref, ok := refs[label]
				if !ok {
					t.Fatalf("observation did not report %q: %v", label, refs)
				}
				if got := resolvedText(t, ref); got != label {
					t.Errorf("ref %q labelled %q but resolved to %q", ref, label, got)
				}
			}
		})
	}

	t.Run("screenshot overlay refs resolve to their own element", func(t *testing.T) {
		boxes, err := (&ScreenshotTool{sessions: sessions, engine: engine}).extractBoundingBoxes(page, false)
		if err != nil {
			t.Fatalf("extractBoundingBoxes failed: %v", err)
		}
		seen := map[string]bool{}
		for _, box := range boxes {
			if box.Label != "30d" && box.Label != "90d" && box.Label != "Search" {
				continue
			}
			seen[box.Label] = true
			if got := resolvedText(t, box.Ref); got != box.Label {
				t.Errorf("overlay %d ref %q labelled %q but resolved to %q", box.Index, box.Ref, box.Label, got)
			}
		}
		if len(seen) != 3 {
			t.Fatalf("screenshot overlays missed a target: saw %v", seen)
		}
	})

	t.Run("a page without a React root reports no dead controls", func(t *testing.T) {
		tool := &GetInteractiveElementsTool{sessions: sessions, engine: engine}
		result, err := tool.Execute(ctx, map[string]interface{}{"session_id": session.ID, "filter": "all"})
		if err != nil {
			t.Fatalf("GetInteractiveElementsTool failed: %v", err)
		}
		out := result.(map[string]interface{})
		if dead, ok := out["dead_controls"]; ok {
			t.Fatalf("dead controls reported on a page with no React root: %v", dead)
		}
		for _, raw := range out["elements"].([]interface{}) {
			if elem := raw.(map[string]interface{}); elem["dead"] != nil {
				t.Fatalf("element flagged dead on a page with no React root: %v", elem)
			}
		}
	})

	t.Run("an element carries the same ref in every observation", func(t *testing.T) {
		buttons := interactiveRefs(t, "buttons")
		all := interactiveRefs(t, "all")
		boxes, err := (&ScreenshotTool{sessions: sessions, engine: engine}).extractBoundingBoxes(page, false)
		if err != nil {
			t.Fatalf("extractBoundingBoxes failed: %v", err)
		}
		overlay := map[string]string{}
		for _, box := range boxes {
			overlay[box.Label] = box.Ref
		}
		for _, label := range []string{"30d", "90d"} {
			if buttons[label] != all[label] || all[label] != overlay[label] {
				t.Errorf("%q has ref %q (buttons), %q (all), %q (screenshot)", label, buttons[label], all[label], overlay[label])
			}
		}
	})
}
