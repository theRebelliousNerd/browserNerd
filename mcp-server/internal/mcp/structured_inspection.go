package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"github.com/go-rod/rod"
	sdkmcp "github.com/mark3labs/mcp-go/mcp"
)

func snapshotInputSchema(schema map[string]interface{}) map[string]interface{} {
	props := schema["properties"].(map[string]interface{})
	for name, definition := range map[string]interface{}{
		"root_selector":      map[string]interface{}{"type": "string", "maxLength": 512, "description": "Deep: unique CSS root; excludes root_ref"},
		"root_ref":           map[string]interface{}{"type": "string", "maxLength": 256, "description": "Deep: observed session ref; excludes root_selector"},
		"max_nodes":          map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500, "description": "Deep rows (default 200; scan 4x)"},
		"max_depth":          map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Deep depth (default 12)"},
		"max_snapshot_bytes": map[string]interface{}{"type": "integer", "minimum": 1024, "maximum": 32768, "description": "Deep JSON bytes, excluding envelope (default 8000)"},
	} {
		props[name] = definition
	}
	if _, exists := props["view"]; !exists {
		props["view"] = map[string]interface{}{"type": "string", "enum": []string{"summary", "compact", "full"}, "description": "Default compact; every view remains bounded"}
	}
	if _, exists := props["max_items"]; !exists {
		props["max_items"] = map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 200, "description": "Returned snapshot rows (default 20)"}
	}
	if _, exists := props["visible_only"]; !exists {
		props["visible_only"] = map[string]interface{}{"type": "boolean", "description": "Visible DOM/host rows only (default false)"}
	}
	if _, exists := props["emit_facts"]; !exists {
		props["emit_facts"] = map[string]interface{}{"type": "boolean", "description": "Optionally submit rows to shared Mangle; retention unknown (default false)"}
	}
	return schema
}

type inspectionOptions struct {
	Capture  browser.SnapshotOptions
	MaxItems int
	MaxBytes int
	View     string
	Selector string
	Ref      string
}

func parseInspectionOptions(args map[string]interface{}) (inspectionOptions, error) {
	o := inspectionOptions{MaxItems: 20, MaxBytes: 8000, View: normalizeProgressiveView(getStringArg(args, "view"))}
	if value, exists := args["view"]; exists {
		s, ok := value.(string)
		if !ok || (s != "summary" && s != "compact" && s != "full") {
			return o, fmt.Errorf("%w: view must be summary, compact or full", sdkmcp.ErrInvalidParams)
		}
	}
	integer := func(name string, fallback, ceiling int) (int, error) {
		value, exists := args[name]
		if !exists {
			return fallback, nil
		}
		var number float64
		switch n := value.(type) {
		case int:
			number = float64(n)
		case float64:
			number = n
		default:
			return 0, fmt.Errorf("%w: %s must be an integer", sdkmcp.ErrInvalidParams, name)
		}
		floor := 1
		if name == "max_snapshot_bytes" {
			floor = 1024
		}
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < float64(floor) || number > float64(ceiling) {
			return 0, fmt.Errorf("%w: %s must be %d..%d", sdkmcp.ErrInvalidParams, name, floor, ceiling)
		}
		return int(number), nil
	}
	var err error
	if o.Capture.MaxNodes, err = integer("max_nodes", 200, 500); err != nil {
		return o, err
	}
	if o.Capture.MaxDepth, err = integer("max_depth", 12, 50); err != nil {
		return o, err
	}
	if o.MaxItems, err = integer("max_items", 20, 200); err != nil {
		return o, err
	}
	if o.MaxBytes, err = integer("max_snapshot_bytes", 8000, 32768); err != nil {
		return o, err
	}
	for name, target := range map[string]*bool{"emit_facts": &o.Capture.EmitFacts, "visible_only": &o.Capture.VisibleOnly} {
		if value, exists := args[name]; exists {
			b, ok := value.(bool)
			if !ok {
				return o, fmt.Errorf("%w: %s must be boolean", sdkmcp.ErrInvalidParams, name)
			}
			*target = b
		}
	}
	for name, target := range map[string]*string{"root_selector": &o.Selector, "root_ref": &o.Ref} {
		if value, exists := args[name]; exists {
			s, ok := value.(string)
			if !ok || strings.TrimSpace(s) == "" || len(s) > 512 || name == "root_ref" && len(s) > 256 {
				return o, fmt.Errorf("%w: %s must be a bounded nonempty string", sdkmcp.ErrInvalidParams, name)
			}
			*target = s
		}
	}
	if o.Selector != "" && o.Ref != "" {
		return o, fmt.Errorf("%w: choose root_selector or root_ref", sdkmcp.ErrInvalidParams)
	}
	return o, nil
}

func inspectionRoot(ctx context.Context, sessions *browser.SessionManager, sessionID string, options inspectionOptions) (*rod.Element, error) {
	page, ok := sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}
	page = page.Context(ctx)
	if options.Ref != "" {
		registry := sessions.Registry(sessionID)
		if registry == nil || registry.Get(options.Ref) == nil {
			return nil, fmt.Errorf("%w: root_ref was not observed in this session", sdkmcp.ErrInvalidParams)
		}
		el, found := resolveGeneratedRef(page, options.Ref, 2*time.Second)
		if !found || !refStillNamesFingerprint(el, registry.Get(options.Ref)) {
			return nil, fmt.Errorf("%w: root_ref is stale or unavailable", sdkmcp.ErrInvalidParams)
		}
		return el, nil
	}
	if options.Selector == "" {
		return nil, nil
	}
	res, err := page.Evaluate(rod.Eval(`(selector)=>{const matches=document.querySelectorAll(selector);if(matches.length!==1)throw new Error('root_selector must match exactly one element; matched '+matches.length);return matches[0];}`, options.Selector).ByObject())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid, missing or ambiguous root_selector: %v", sdkmcp.ErrInvalidParams, err)
	}
	if res == nil || res.ObjectID == "" {
		return nil, fmt.Errorf("%w: root_selector has no live element", sdkmcp.ErrInvalidParams)
	}
	return page.ElementFromObject(res)
}

func inspectionResponse(snapshot *browser.StructuredSnapshot, options inspectionOptions, sessionID string) (map[string]interface{}, error) {
	scope := map[string]interface{}{"kind": "document_prefix", "traversal": "preorder_prefix", "visible_only": options.Capture.VisibleOnly, "fields": "bounded_scalars_and_function_presence"}
	if options.Selector != "" {
		scope["kind"], scope["root_selector"] = "subtree", options.Selector
	}
	if options.Ref != "" {
		scope["kind"], scope["root_ref"] = "subtree", options.Ref
	}
	count := len(snapshot.Nodes)
	if count > options.MaxItems {
		count = options.MaxItems
	}
	if options.View == "summary" {
		count = 0
	}
	reasons := append([]string{}, snapshot.Reasons...)
	if count < len(snapshot.Nodes) {
		if options.View == "summary" {
			reasons = append(reasons, "summary_omits_rows")
		} else {
			reasons = append(reasons, "response_row_limit")
		}
	}
	response := map[string]interface{}{"success": true, "status": "captured", "session_id": sessionID, "scope": scope, "scanned": snapshot.Scanned, "captured": snapshot.Captured, "returned": count, "capture_complete": snapshot.CaptureComplete, "capability": snapshot.Capability, "fact_storage": snapshot.FactStorage, "budgets": map[string]interface{}{"max_nodes": options.Capture.MaxNodes, "max_depth": options.Capture.MaxDepth, "max_scanned": options.Capture.MaxNodes * 4, "max_items": options.MaxItems, "max_snapshot_bytes": options.MaxBytes}}
	response["discovery_scanned"] = snapshot.DiscoveryScanned
	response["capture_id"] = snapshot.CaptureID
	response["view"] = options.View
	response["budgets"].(map[string]interface{})["max_discovery_scanned"] = options.Capture.MaxNodes * 4
	for {
		response["nodes"] = snapshot.Nodes[:count]
		response["returned"] = count
		response["response_complete"] = count == snapshot.Captured
		response["complete"] = snapshot.CaptureComplete && count == snapshot.Captured
		response["truncated"] = response["complete"] != true
		response["incomplete_reasons"] = reasons
		encoded, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= options.MaxBytes {
			return response, nil
		}
		if count == 0 {
			return nil, fmt.Errorf("snapshot byte budget cannot represent scope metadata")
		}
		if count == len(snapshot.Nodes) || len(reasons) == 0 || reasons[len(reasons)-1] != "response_byte_limit" {
			reasons = append(reasons, "response_byte_limit")
		}
		count--
	}
}

func executeStructuredInspection(ctx context.Context, sessions *browser.SessionManager, sessionID string, args map[string]interface{}, react bool) (interface{}, error) {
	options, err := parseInspectionOptions(args)
	if err != nil {
		return nil, err
	}
	root, err := inspectionRoot(ctx, sessions, sessionID, options)
	if err != nil {
		return nil, err
	}
	var snapshot *browser.StructuredSnapshot
	if react {
		snapshot, err = sessions.ReifyReact(ctx, sessionID, root, options.Capture)
	} else {
		snapshot, err = sessions.SnapshotDOM(ctx, sessionID, root, options.Capture)
	}
	if err != nil {
		return nil, err
	}
	return inspectionResponse(snapshot, options, sessionID)
}
