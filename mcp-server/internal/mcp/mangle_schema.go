package mcp

import (
	"fmt"
	"sort"

	"browsernerd-mcp-server/internal/mangle"
)

// schemaNamesOnlyThreshold is the listing size above which the compact view
// shows names per module instead of full signatures, so an unfiltered call
// stays cheap and a filtered one carries the argument names.
const schemaNamesOnlyThreshold = 40

// mangleSchemaResponse answers browser-mangle operation schema: the
// predicates an agent can query, with argument names, kind and module.
func mangleSchemaResponse(engine *mangle.Engine, filter, view string) map[string]interface{} {
	predicates := engine.Schema(filter)
	response := map[string]interface{}{
		"success":   true,
		"operation": "schema",
		"view":      view,
		"count":     len(predicates),
	}
	if filter != "" {
		response["filter"] = filter
	}

	byModule := make(map[string][]mangle.PredicateInfo)
	for _, p := range predicates {
		byModule[p.Module] = append(byModule[p.Module], p)
	}
	response["summary"] = fmt.Sprintf("%d predicate(s) in %d module(s)", len(predicates), len(byModule))
	if len(predicates) == 0 {
		response["hint"] = "no predicate name contains the filter; call schema without a filter to list every module"
		return response
	}

	switch view {
	case "summary":
		counts := make(map[string]int, len(byModule))
		for module, list := range byModule {
			counts[module] = len(list)
		}
		response["modules"] = counts
	case "full":
		response["predicates"] = predicates
	default:
		if len(predicates) > schemaNamesOnlyThreshold {
			names := make(map[string][]string, len(byModule))
			for module, list := range byModule {
				for _, p := range list {
					names[module] = append(names[module], p.Name)
				}
				sort.Strings(names[module])
			}
			response["modules"] = names
			response["hint"] = `pass filter (e.g. "net_", "fail", "toast") for argument names`
			return response
		}
		lines := make([]string, 0, len(predicates))
		for _, p := range predicates {
			line := fmt.Sprintf("%s  %s %s", p.Signature, p.Kind, p.Module)
			if p.Facts > 0 {
				line += fmt.Sprintf(" facts=%d", p.Facts)
			}
			lines = append(lines, line)
		}
		response["predicates"] = lines
	}
	return response
}
