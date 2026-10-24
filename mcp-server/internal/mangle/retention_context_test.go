package mangle

import (
	"testing"
	"time"
)

func TestDocumentContextRetainsNavigationAndRequestUnits(t *testing.T) {
	now := time.Now()
	facts := []Fact{
		{Predicate: "net_request", Args: []interface{}{"s", "r", "GET", "http://fixture/records", "", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_request_context", Args: []interface{}{"s", "r", "Document", "main", "loader"}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s", "r", 200, 1, 1}, Timestamp: now},
		{Predicate: "navigation_context", Args: []interface{}{"s", "http://fixture/records", "document", "loader", now.UnixMilli()}, Timestamp: now},
	}
	classes := classifyFacts(facts)
	if classes[3] != signalHigh {
		t.Fatalf("document navigation identity must survive low-signal eviction: %v", classes)
	}
	drop, _ := evictionPlan(facts, classes, 3)
	if !drop[0] || !drop[1] || !drop[2] || drop[3] {
		t.Fatalf("request context split from lifecycle or navigation was evicted first: %v", drop)
	}
}
