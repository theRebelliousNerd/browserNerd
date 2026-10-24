package mcp

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReasonScopeLabelsEveryTopicAndView(t *testing.T) {
	for _, clean := range []bool{false, true} {
		for _, view := range []string{"summary", "compact", "full"} {
			for _, topic := range reasonTopics {
				t.Run(fmt.Sprintf("clean=%t/%s/%s", clean, view, topic), func(t *testing.T) {
					engine := testMangleEngineForProgressive(t)
					p := newProbeFacts("scope-labels")
					status := 502
					if clean {
						status = 200
					}
					p.request("records", "GET", probeOrigin+"/api/records", 1000, status, 1)
					loadFacts(t, engine, p)
					tool := &BrowserReasonTool{engine: engine}
					before := time.Now().UnixMilli() - defaultReasonTimeWindowMs
					res := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": topic, "view": view, "since_navigation": false})
					after := time.Now().UnixMilli() - defaultReasonTimeWindowMs
					label := fmt.Sprint(res["scope"])
					if label != "in the last 300s of session history" {
						t.Errorf("scope omitted or unclear: %v", res)
					}
					if since := asInt64(res["scope_since_ms"]); since < before || since > after {
						t.Errorf("effective window lower bound absent or wrong: %v", res)
					}
					if topic != "network" {
						verdict := fmt.Sprint(res["verdict"])
						if !strings.Contains(verdict, label) || !clean && !strings.HasPrefix(verdict, label+": ") {
							t.Errorf("verdict did not state its scope: %v", res)
						}
					}
				})
			}
		}
	}
}

func TestReasonEffectiveScopeLabelFollowsLatestBound(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		nav, click int64
		window     int
	}{
		{"navigation_wins", "since the last navigation", 10000, 1000, 300000},
		{"action_wins", "since your last action", 1000, 10000, 300000},
		{"window_wins", "in the last 500ms of session history", 1000, 2000, 500},
		{"all_history", "in all retained session history", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := testMangleEngineForProgressive(t)
			p := newProbeFacts("effective-scope")
			if tc.nav > 0 {
				p.add("navigation_event", tc.nav, probeOrigin+"/records", p.at(tc.nav).UnixMilli())
			}
			if tc.click > 0 {
				p.add("click_event", tc.click, "records", p.at(tc.click).UnixMilli())
			}
			loadFacts(t, engine, p)
			tool := &BrowserReasonTool{engine: engine}
			res := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": "health", "since_action": true, "since_navigation": true, "time_window_ms": tc.window})
			if res["scope"] != tc.want || !strings.Contains(fmt.Sprint(res["verdict"]), tc.want) {
				t.Fatalf("scope does not describe actual latest bound: %+v", res)
			}
			want := int64(0)
			if tc.name == "navigation_wins" {
				want = p.at(tc.nav).UnixMilli()
			} else if tc.name == "action_wins" {
				want = p.at(tc.click).UnixMilli() - actionScopeSlackMs
			} else if tc.name == "window_wins" {
				if asInt64(res["scope_since_ms"]) < time.Now().UnixMilli()-600 {
					t.Fatalf("short window did not set the actual bound: %+v", res)
				}
				return
			}
			if asInt64(res["scope_since_ms"]) != want {
				t.Fatalf("actual lower bound differs from selected scope: %+v", res)
			}
		})
	}
}
