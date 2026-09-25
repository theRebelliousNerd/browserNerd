package mcp

import (
	"fmt"
	"strings"
)

// compactObserveState keeps what identifies the page and flags only what is unusual:
// a page still loading, or an open dialog.
func compactObserveState(state map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"url":   state["url"],
		"title": state["title"],
	}
	if loading, ok := state["loading"].(bool); ok && loading {
		out["loading"] = true
	}
	if dialog, ok := state["hasDialog"].(bool); ok && dialog {
		out["hasDialog"] = true
	}
	return out
}

// compactObserveInteractive shapes grouped interactive data for compact output: the
// true totals, at most maxItems rows, and any dead controls. summary.returned counts
// the rows this response carries, never the rows the page extraction held.
func compactObserveInteractive(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := map[string]interface{}{}
	elems := toAnySlice(data["elements"])
	shown := limitAnySlice(elems, maxItems)
	if source, ok := data["summary"].(map[string]interface{}); ok {
		summary := make(map[string]interface{}, len(source))
		for k, v := range source {
			summary[k] = v
		}
		summary["returned"] = len(shown)
		out["summary"] = summary
		if asInt(source["groups"]) > len(shown) || (source["groups"] == nil && asInt(source["total"]) > len(shown)) {
			out["truncated"] = true
		}
	}
	out["elements"] = shown
	if dead, ok := data["dead_controls"]; ok {
		out["dead_controls"] = dead
	}
	return out
}

// annotateCandidatesFromRows marks each candidate with what the current interactive
// extraction says about its ref: how many controls its group holds, and whether it is
// dead. The rows are fresher than any fact, so they win over annotateCandidatesFromFacts.
func annotateCandidatesFromRows(candidates []map[string]interface{}, interactive map[string]interface{}) {
	if len(candidates) == 0 {
		return
	}
	type rowInfo struct {
		count int
		dead  bool
	}
	rows := map[string]rowInfo{}
	for _, raw := range toAnySlice(interactive["elements"]) {
		row, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		dead, _ := row["dead"].(bool)
		rows[getStringFromMap(row, "ref")] = rowInfo{count: asInt(row["count"]), dead: dead}
	}
	for _, c := range candidates {
		info, ok := rows[fmt.Sprintf("%v", c["ref"])]
		if !ok {
			continue
		}
		if info.count > 1 {
			c["count"] = info.count
		}
		if info.dead {
			c["dead"] = true
		}
	}
}

// observeSummaryLine says in one line what an observation found: the page, how many
// controls in how many distinct groups, dead controls, and the health digest.
func observeSummaryLine(state, interactive, nav, health map[string]interface{}, candidates int) string {
	parts := make([]string, 0, 5)
	if title := strings.TrimSpace(fmt.Sprintf("%v", state["title"])); title != "" && title != "<nil>" {
		parts = append(parts, title)
	}
	if loading, ok := state["loading"].(bool); ok && loading {
		parts = append(parts, "still loading")
	}
	if summary, ok := interactive["summary"].(map[string]interface{}); ok {
		line := fmt.Sprintf("%d control(s)", asInt(summary["total"]))
		if groups := asInt(summary["groups"]); groups > 0 && groups < asInt(summary["total"]) {
			line += fmt.Sprintf(" in %d group(s)", groups)
		}
		parts = append(parts, line)
	}
	if dead, ok := interactive["dead_controls"].(map[string]interface{}); ok {
		parts = append(parts, fmt.Sprintf("%d dead control(s)", asInt(dead["count"])))
	}
	if counts, ok := nav["counts"].(map[string]interface{}); ok {
		if total := asInt(counts["total"]); total > 0 {
			parts = append(parts, fmt.Sprintf("%d link(s)", total))
		}
	}
	if line := healthSummary(health); line != "" {
		parts = append(parts, "health: "+line)
	}
	if candidates > 0 {
		parts = append(parts, fmt.Sprintf("%d distinct action(s)", candidates))
	}
	if len(parts) == 0 {
		return "observation complete"
	}
	return strings.Join(parts, "; ")
}
