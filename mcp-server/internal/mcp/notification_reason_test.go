package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReasonLiveRegionErrorsKeepSessionAndTimeScope(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("owned")
	p.add("live_region_notification", 100, "Stale validation", "alert", "error", p.at(100).UnixMilli())
	p.add("live_region_notification", 2000, "Account input invalid", "alert", "error", p.at(2000).UnixMilli())
	p.add("live_region_notification", 2100, "Membership active", "status", "info", p.at(2100).UnixMilli())
	loadFacts(t, engine, p)
	other := newProbeFacts("foreign")
	other.add("live_region_notification", 2200, "Foreign validation", "alert", "error", other.at(2200).UnixMilli())
	loadFacts(t, engine, other)
	ev := collectBrowserEvidence(context.Background(), engine, reasonScope{sessionID: "owned", sinceMs: p.at(1000).UnixMilli(), strict: true})
	d := diagnose(ev)
	if d.score.liveRegionErrors != 1 || d.score.toastErrors != 0 {
		t.Fatalf("wrong classifications or scope: %+v", d.score.row())
	}
	rows := fmt.Sprint(d.section("live_regions", 20))
	if !strings.Contains(rows, "role:alert") || !strings.Contains(rows, "Account input invalid") || strings.Contains(rows, "Stale") || strings.Contains(rows, "Foreign") {
		t.Fatalf("live region evidence lost role or leaked scope: %s", rows)
	}
	if primary := d.primary(); primary == nil || primary.kind != "live_region_error" || !strings.Contains(primary.line, "live region error") {
		t.Fatalf("inline error did not reach diagnosis: %+v", primary)
	}
	if rows := d.section("toasts", 20); rows != nil {
		t.Fatalf("inline errors became toast evidence: %+v", rows)
	}
}
