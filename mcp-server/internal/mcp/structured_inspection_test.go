package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/browser"
)

func TestStructuredInspectionProviderSchemas(t *testing.T) {
	for _, tool := range []Tool{&BrowserObserveTool{}, &ReifyReactTool{}, &SnapshotDOMTool{}} {
		props := tool.InputSchema()["properties"].(map[string]interface{})
		for _, key := range []string{"root_selector", "root_ref", "max_nodes", "max_depth", "max_items", "max_snapshot_bytes", "visible_only", "emit_facts", "gate_reason", "approved_by_handle"} {
			if props[key] == nil {
				t.Fatalf("%s does not advertise %s", tool.Name(), key)
			}
		}
	}
}

func TestStructuredInspectionStrictOptions(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"max_nodes": 0}, {"max_nodes": 501}, {"max_nodes": 1.5}, {"max_depth": 51}, {"max_items": 201}, {"max_snapshot_bytes": 1023},
		{"max_snapshot_bytes": 32769}, {"max_nodes": "200"}, {"visible_only": "true"}, {"emit_facts": nil}, {"root_selector": ""},
		{"root_ref": true}, {"root_selector": "main", "root_ref": "tab"}, {"root_selector": strings.Repeat("x", 513)},
	} {
		if _, err := parseInspectionOptions(args); err == nil {
			t.Fatalf("accepted invalid options: %#v", args)
		}
	}
	opts, err := parseInspectionOptions(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Capture.EmitFacts || opts.Capture.VisibleOnly || opts.Capture.MaxNodes != 200 || opts.MaxItems != 20 || opts.MaxBytes != 8000 {
		t.Fatalf("incorrect defaults: %+v", opts)
	}
	for _, mode := range []string{"react", "dom_snapshot"} {
		out, err := (&BrowserObserveTool{}).Execute(context.Background(), map[string]interface{}{"session_id": "gate", "mode": mode, "root_selector": "main", "max_nodes": 100000})
		if err != nil || asMap(out)["gated"] != true {
			t.Fatalf("options bypassed disclosure gate: %+v %v", out, err)
		}
	}
}

func TestStructuredInspectionAllViewsBoundedAndHonest(t *testing.T) {
	nodes := make([]map[string]interface{}, 200)
	for i := range nodes {
		nodes[i] = map[string]interface{}{"id": fmt.Sprint(i), "props": map[string]interface{}{"value": "calendar", "onValueChange": map[string]interface{}{"type": "function"}, "description": strings.Repeat("x", 256)}}
	}
	for _, view := range []string{"summary", "compact", "full"} {
		for _, budget := range []int{1024, 2048, 8000} {
			opts, err := parseInspectionOptions(map[string]interface{}{"view": view, "max_snapshot_bytes": budget, "max_items": 200, "root_selector": "main"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := inspectionResponse(&browser.StructuredSnapshot{Nodes: nodes, Captured: 200, Scanned: 200, CaptureComplete: false, Reasons: []string{"node_limit"}, Capability: "available"}, opts, "own")
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(out)
			if len(encoded) > budget {
				t.Fatalf("%s exceeded %d-byte snapshot budget: %d", view, budget, len(encoded))
			}
			if out["truncated"] != true || out["complete"] != false || out["capture_complete"] != false || out["captured"] != 200 || out["scope"] == nil {
				t.Fatalf("capture incompleteness lost: %+v", out)
			}
			if view == "summary" && out["returned"] != 0 {
				t.Fatal("summary exposed rows")
			}
			wrapped := deepObservationResponse("react", view, out, nil)
			if wrapped["success"] != true || wrapped["status"] != "partial" || wrapped["truncated"] != true {
				t.Fatalf("wrapper hid partial evidence: %+v", wrapped)
			}
		}
	}
}

func TestStructuredInspectionUnavailableIsNotEmptySuccess(t *testing.T) {
	opts, _ := parseInspectionOptions(map[string]interface{}{"view": "full"})
	out, err := inspectionResponse(&browser.StructuredSnapshot{Nodes: []map[string]interface{}{}, Capability: "unavailable", CaptureComplete: false, Reasons: []string{"no_accessible_react_root_within_scan_budget"}}, opts, "own")
	if err != nil {
		t.Fatal(err)
	}
	if out["complete"] != false || out["capture_complete"] != false || out["capability"] != "unavailable" {
		t.Fatalf("missing evidence fabricated: %+v", out)
	}
}
