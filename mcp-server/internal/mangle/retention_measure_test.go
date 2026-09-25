package mangle

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

// pollingWorkload replays the fact mix of an app that polls: every request
// brings net_request, request_initiator, 6 request headers, net_response and
// 5 response headers; every 25th poll fails with a 500 and a console error;
// every 200th poll lands a 200-node DOM snapshot (a page load).
func pollingWorkload(n int, start time.Time) [][]Fact {
	batches := make([][]Fact, 0, n*2)
	for i := 0; i < n; i++ {
		ts := start.Add(time.Duration(i) * 400 * time.Millisecond)
		ms := ts.UnixMilli()
		id := fmt.Sprintf("req-%d", i)
		url := "http://app.test/api/poll?x=1"
		status := int64(200)
		if i%25 == 24 {
			url = "http://app.test/api/save"
			status = 500
		}
		req := []Fact{
			{Predicate: "net_request", Args: []interface{}{"s1", id, "GET", url, "http://app.test/main.js:40", ms}, Timestamp: ts},
			{Predicate: "request_initiator", Args: []interface{}{"s1", id, "script", "http://app.test/main.js:40"}, Timestamp: ts},
		}
		for h := 0; h < 6; h++ {
			req = append(req, Fact{Predicate: "net_header", Args: []interface{}{"s1", id, "req", fmt.Sprintf("h%d", h), "v"}, Timestamp: ts})
		}
		batches = append(batches, req)
		rts := ts.Add(30 * time.Millisecond)
		res := []Fact{{Predicate: "net_response", Args: []interface{}{"s1", id, status, int64(30), int64(5)}, Timestamp: rts}}
		for h := 0; h < 5; h++ {
			res = append(res, Fact{Predicate: "net_header", Args: []interface{}{"s1", id, "res", fmt.Sprintf("h%d", h), "v"}, Timestamp: rts})
		}
		if status >= 400 {
			res = append(res,
				Fact{Predicate: "net_http_error", Args: []interface{}{"s1", id, url, status, "Fetch", rts.UnixMilli()}, Timestamp: rts},
				Fact{Predicate: "console_event", Args: []interface{}{"s1", "error", "Save failed with status 500", rts.UnixMilli() + 2}, Timestamp: rts.Add(2 * time.Millisecond)},
			)
		}
		batches = append(batches, res)
		if i%200 == 0 {
			dom := make([]Fact, 0, 800)
			for node := 0; node < 200; node++ {
				nid := fmt.Sprintf("node_%d", node)
				dom = append(dom,
					Fact{Predicate: "dom_node", Args: []interface{}{"s1", nid, "DIV", "text", "root"}, Timestamp: ts},
					Fact{Predicate: "dom_text", Args: []interface{}{"s1", nid, "text"}, Timestamp: ts},
					Fact{Predicate: "dom_attr", Args: []interface{}{"s1", nid, "class", "card"}, Timestamp: ts},
					Fact{Predicate: "dom_layout", Args: []interface{}{"s1", nid, 0.0, float64(node * 20), 100.0, 20.0, "true"}, Timestamp: ts},
				)
			}
			batches = append(batches, dom)
		}
	}
	return batches
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// TestMeasureRetentionCost prints AddFacts and read latency at several buffer
// limits under the polling workload. Run with BROWSERNERD_MEASURE=1.
func TestMeasureRetentionCost(t *testing.T) {
	if os.Getenv("BROWSERNERD_MEASURE") == "" {
		t.Skip("set BROWSERNERD_MEASURE=1 to measure")
	}
	limits := []int{2048, 4096, 8192, 16384}
	if raw := os.Getenv("BROWSERNERD_MEASURE_LIMIT"); raw != "" {
		var only int
		if _, err := fmt.Sscan(raw, &only); err == nil && only > 0 {
			limits = []int{only}
		}
	}
	for _, limit := range limits {
		engine, err := NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: limit, EvaluationTimeout: "30s"})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		polls := limit / 5 * 3
		batches := pollingWorkload(polls, time.Now().Add(-time.Hour))
		lat := make([]time.Duration, 0, len(batches))
		start := time.Now()
		for _, batch := range batches {
			t0 := time.Now()
			if err := engine.AddFacts(ctx, batch); err != nil {
				t.Fatalf("limit %d: AddFacts: %v", limit, err)
			}
			lat = append(lat, time.Since(t0))
		}
		total := time.Since(start)
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })

		// Each read follows a fresh batch, as live capture does between reads,
		// so every read pays its evaluation.
		timeRead := func(query string) (time.Duration, int, error) {
			var worst time.Duration
			var rows int
			var readErr error
			for r := 0; r < 5; r++ {
				_ = engine.AddFacts(ctx, batches[len(batches)-1-r])
				t0 := time.Now()
				res, err := engine.Query(ctx, query)
				if d := time.Since(t0); d > worst {
					worst = d
				}
				rows, readErr = len(res), err
			}
			return worst, rows, readErr
		}
		causedMax, causedRows, causedErr := timeRead("caused_by(S, Msg, R).")
		joinMax, joinRows, joinErr := timeRead("net_request(S, R, M, U, I, T), net_response(S, R, 500, L, D).")
		reads := make([]time.Duration, 0, 5)
		var rows int
		var readErr error
		for r := 0; r < 5; r++ {
			_ = engine.AddFacts(ctx, batches[len(batches)-1-r])
			t0 := time.Now()
			res, err := engine.Query(ctx, "failed_request(S, R, U, St).")
			reads = append(reads, time.Since(t0))
			rows, readErr = len(res), err
		}
		sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
		t.Logf("limit=%5d reads after a write: caused_by max=%s rows=%d err=%v | conjunction max=%s rows=%d err=%v | stats=%+v",
			limit, causedMax, causedRows, causedErr, joinMax, joinRows, joinErr, engine.Stats())
		failures := 0
		for _, f := range engine.FactsByPredicate("net_response") {
			if fmt.Sprintf("%v", f.Args[2]) == "500" {
				failures++
			}
		}
		t.Logf("limit=%5d batches=%d facts=%d | AddFacts p50=%s p95=%s max=%s total=%s | read(failed_request) p50=%s max=%s rows=%d err=%v | 500s retained=%d of %d sent",
			limit, len(batches), len(engine.Facts()),
			percentile(lat, 0.5), percentile(lat, 0.95), lat[len(lat)-1], total.Round(time.Millisecond),
			percentile(reads, 0.5), reads[len(reads)-1], rows, readErr, failures, polls/25)
	}
}
