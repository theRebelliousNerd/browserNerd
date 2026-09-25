package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// uploadFiles sets the files of an <input type="file"> through CDP
// DOM.setFileInputFiles, the way a person choosing files in the picker does.
//
// It is the structured alternative to the gated js op: a page's upload flow can
// be tested without arbitrary script. Every path goes through
// PathPolicy.ResolveForUpload before the browser sees it, so only regular files
// inside the workspace root, outside every dot path and not named as a
// credential, can reach a page.
func uploadFiles(
	ctx context.Context,
	sessions *browser.SessionManager,
	engine *mangle.Engine,
	policy *security.PathPolicy,
	sessionID, ref string,
	rawFiles interface{},
) (map[string]interface{}, error) {
	if sessionID == "" || ref == "" {
		return nil, fmt.Errorf("session_id and ref are required")
	}
	requested := toStringSlice(rawFiles)
	if len(requested) == 0 {
		return map[string]interface{}{"success": false, "error": "files must list at least one path"}, nil
	}

	resolved := make([]string, 0, len(requested))
	for _, path := range requested {
		file, err := policy.ResolveForUpload(path)
		if err != nil {
			return map[string]interface{}{"success": false, "error": err.Error()}, nil
		}
		resolved = append(resolved, file)
	}

	page, ok := sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	element, err := findElementByRefWithRegistry(page, ref, sessions.Registry(sessionID))
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}

	tagName := ""
	if prop, propErr := element.Property("tagName"); propErr == nil {
		tagName = strings.ToUpper(prop.Str())
	}
	inputType := ""
	if attr, attrErr := element.Attribute("type"); attrErr == nil && attr != nil {
		inputType = strings.ToLower(*attr)
	}
	if tagName != "INPUT" || inputType != "file" {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("%s is not a file input (tag %q, type %q)", ref, tagName, inputType),
		}, nil
	}
	if len(resolved) > 1 {
		if multiple, multiErr := element.Attribute("multiple"); multiErr != nil || multiple == nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("%s accepts one file and %d were given", ref, len(resolved)),
			}, nil
		}
	}

	if err := element.Timeout(10 * time.Second).SetFiles(resolved); err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Set files failed: %v", err)}, nil
	}

	names := make([]string, 0, len(resolved))
	for _, file := range resolved {
		names = append(names, filepath.Base(file))
	}

	now := time.Now()
	if engine != nil {
		_ = engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "user_upload",
			Args:      []interface{}{sessionID, ref, len(resolved), now.UnixMilli()},
			Timestamp: now,
		}})
	}

	return map[string]interface{}{"success": true, "ref": ref, "action": "upload", "files": names}, nil
}
