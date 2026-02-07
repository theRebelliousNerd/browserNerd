package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	resourceMIMEJSON = "application/json"
)

func (s *Server) registerAllResources() {
	if s == nil || s.mcpServer == nil {
		return
	}

	s.mcpServer.AddResource(
		mcp.NewResource(
			"browsernerd://about",
			"BrowserNERD About",
			mcp.WithMIMEType(resourceMIMEJSON),
			mcp.WithResourceDescription("High-level server info and usage notes."),
		),
		s.handleAboutResource,
	)

	s.mcpServer.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"browsernerd://session/{sessionId}/facts{?predicate,limit}",
			"Session Facts",
			mcp.WithTemplateMIMEType(resourceMIMEJSON),
			mcp.WithTemplateDescription("Read a token-efficient slice of facts for a session (optionally filtered by predicate)."),
		),
		s.handleSessionFactsResource,
	)
}

func (s *Server) handleAboutResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	payload := map[string]interface{}{
		"name":    s.cfg.Server.Name,
		"version": s.cfg.Server.Version,
		"notes": []string{
			"Resources are read-only context endpoints; use tools for actions/mutations.",
			"Resource templates are parameterized resources (URI templates) for session-scoped reads.",
			"For best token efficiency, use session-scoped reads (session_id or {sessionId}).",
		},
		"timestamp_ms": time.Now().UnixMilli(),
	}

	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: resourceMIMEJSON,
			Text:     string(text),
		},
	}, nil
}

func (s *Server) handleSessionFactsResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("mangle engine unavailable")
	}

	sessionID := argString(request.Params.Arguments["sessionId"])
	if sessionID == "" {
		return nil, fmt.Errorf("missing sessionId")
	}
	predicate := argString(request.Params.Arguments["predicate"])
	limit := asInt(request.Params.Arguments["limit"])
	if limit <= 0 {
		limit = 25
	}
	if limit > 500 {
		limit = 500
	}

	facts := selectRecentSessionFacts(s.engine, sessionID, predicate, limit)

	payload := map[string]interface{}{
		"session_id": sessionID,
		"predicate":  predicate,
		"limit":      limit,
		"count":      len(facts),
		"facts":      facts,
	}
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: resourceMIMEJSON,
			Text:     string(text),
		},
	}, nil
}

func selectRecentSessionFacts(engine *mangle.Engine, sessionID, predicate string, limit int) []mangle.Fact {
	if engine == nil || sessionID == "" || limit <= 0 {
		return []mangle.Fact{}
	}

	var source []mangle.Fact
	if predicate != "" {
		source = engine.FactsByPredicate(predicate)
	} else {
		source = engine.Facts()
	}

	out := make([]mangle.Fact, 0, min(limit, len(source)))
	for i := len(source) - 1; i >= 0 && len(out) < limit; i-- {
		f := source[i]
		if predicate != "" && f.Predicate != predicate {
			continue
		}
		if len(f.Args) == 0 {
			continue
		}
		if fmt.Sprintf("%v", f.Args[0]) != sessionID {
			continue
		}
		out = append(out, f)
	}

	// Reverse to return chronological order (oldest -> newest).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func argString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case []string:
		if len(value) == 0 {
			return ""
		}
		return value[0]
	default:
		return fmt.Sprintf("%v", value)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

