package logs

import (
	"sort"
	"strings"
	"time"
)

type Match struct {
	Entry  Entry  `json:"entry"`
	Reason string `json:"reason"`
}

// MatchRequestLines ranks matching request paths before nearby error evidence.
// Entries without measured timestamps never enter a time-based correlation.
func MatchRequestLines(entries []Entry, path string, at time.Time, window, errorWindow time.Duration, max int) []Match {
	if max <= 0 || at.IsZero() {
		return nil
	}
	path = strings.TrimSpace(path)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	var needles []string
	if path != "" && path != "/" {
		needles = append(needles, path)
		segments := strings.Split(strings.Trim(path, "/"), "/")
		if len(segments) >= 3 {
			needles = append(needles, "/"+strings.Join(segments[1:], "/"))
		}
	}
	type candidate struct {
		match Match
		dist  time.Duration
		rank  int
	}
	seen := make(map[string]bool)
	var candidates []candidate
	for _, entry := range entries {
		if entry.Timestamp.IsZero() {
			continue
		}
		distance := entry.Timestamp.Sub(at)
		if distance < 0 {
			distance = -distance
		}
		text := entry.Message
		if text == "" {
			text = entry.Raw
		}
		key := entry.Source + "|" + entry.Timestamp.String() + "|" + text
		if seen[key] {
			continue
		}
		pathMatch := false
		for _, needle := range needles {
			if strings.Contains(text, needle) || strings.Contains(entry.Raw, needle) {
				pathMatch = true
				break
			}
		}
		if distance <= window && pathMatch {
			seen[key] = true
			candidates = append(candidates, candidate{Match{entry, "path"}, distance, 0})
		} else if errorWindow > 0 && distance <= errorWindow && (entry.Level == "ERROR" || entry.Level == "CRITICAL") {
			seen[key] = true
			candidates = append(candidates, candidate{Match{entry, "time"}, distance, 1})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].dist < candidates[j].dist
	})
	if len(candidates) > max {
		candidates = candidates[:max]
	}
	result := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.match)
	}
	return result
}
