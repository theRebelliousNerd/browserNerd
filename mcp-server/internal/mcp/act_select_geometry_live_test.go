package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveOptionClickTracksScrollAffordanceGeometry(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "probe", "scroll-select.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"stationary_control", "role", "ref", "select", "coordinates", "hover_relayout", "unstable", "detached"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			act, sid := liveDataPage(t, ctx, string(fixture))
			page, _ := act.sessions.Page(sid)
			if mode == "stationary_control" || mode == "hover_relayout" || mode == "unstable" || mode == "detached" {
				if _, err := page.Eval(`() => { window.stationaryControl = true; }`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "hover_relayout" {
				if _, err := page.Eval(`() => { window.moveOnHover = true; }`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unstable" || mode == "detached" {
				if _, err := page.Eval(`(mode) => { window.unstableOnHover = mode === 'unstable'; window.detachOnHover = mode === 'detached'; }`, mode); err != nil {
					t.Fatal(err)
				}
			}
			run := func(op map[string]interface{}) map[string]interface{} {
				t.Helper()
				res, err := act.Execute(ctx, map[string]interface{}{"session_id": sid, "operations": ops(op), "settle_ms": 0})
				if err != nil {
					t.Fatal(err)
				}
				return asMap(res)
			}
			if mode != "select" {
				if res := run(map[string]interface{}{"type": "click", "ref": "trigger"}); res["success"] != true {
					t.Fatal(res)
				}
			}
			op := map[string]interface{}{"type": "click", "role": "option", "name": "Choice 00", "exact": true}
			want := "Choice 00"
			if mode == "ref" {
				op = map[string]interface{}{"type": "click", "ref": "choice-00"}
			}
			if mode == "select" {
				op = map[string]interface{}{"type": "select", "ref": "trigger", "value": "Choice 00"}
			}
			if mode == "coordinates" {
				point, err := page.Eval(`() => { const r=document.getElementById('choice-09').getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; }`)
				if err != nil {
					t.Fatal(err)
				}
				op = map[string]interface{}{"type": "click", "x": point.Value.Get("x").Num(), "y": point.Value.Get("y").Num()}
				want = "Choice 09"
			}
			result := run(op)
			state, err := page.Eval(`() => ({shown:document.getElementById('selected').textContent,native:document.getElementById('native').value,downs:window.optionPointerDowns,events:window.geometryEvents})`)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unstable" || mode == "detached" {
				if result["success"] != false || state.Value.Get("downs").Int() != 0 || state.Value.Get("native").Str() != "Choice 08" {
					t.Fatalf("unsafe target received a press: result=%+v state=%s", result, state.Value.JSON("", ""))
				}
				message := getStringFromMap(firstResult(t, result), "error")
				if mode == "detached" && !strings.Contains(message, "detached") || mode == "unstable" && !strings.Contains(message, "stable") {
					t.Fatalf("refusal did not name geometry cause: %s", message)
				}
				return
			}
			if result["success"] != true || state.Value.Get("shown").Str() != want || state.Value.Get("native").Str() != want {
				t.Fatalf("wrong option after scroll geometry changed: result=%+v state=%s", result, state.Value.JSON("", ""))
			}
			if !state.Value.Get("events").Arr()[len(state.Value.Get("events").Arr())-1].Get("trusted").Bool() {
				t.Fatal("selection did not use trusted native input")
			}
			if state.Value.Get("downs").Int() != 1 {
				t.Fatalf("target click replayed a native press: %s", state.Value.JSON("", ""))
			}
		})
	}
}
