package mcp

import (
	"fmt"
	"strings"
	"testing"
)

func TestLiveNotificationsKeepInlineRegionsSeparate(t *testing.T) {
	p := startLiveProbe(t)
	fixture := `<!doctype html><title>Notification classification</title><h1>Settings</h1>
<span role="status">Membership active</span>
<p id="validation" class="validation-error" role="alert">Required field missing</p>
<div data-route-announcer role="alert">Route changed</div>
<button id="noop">No change</button>
<button id="inline" onclick="const e=document.createElement('p');e.setAttribute('role','alert');e.className='validation-error';e.textContent='Account input invalid';document.body.appendChild(e)">Inline</button>
<button id="update" onclick="document.getElementById('validation').textContent='Email address invalid'">Update validation</button>
<section id="notifications" aria-live="polite"></section>
<button id="toast" onclick="const e=document.createElement('div');e.setAttribute('data-sonner-toast','');e.setAttribute('data-type','success');e.setAttribute('role','status');e.textContent='Settings saved';document.getElementById('notifications').appendChild(e)">Notify</button>`
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "navigate", "url": "data:text/html," + fixture})})
	effects := asMap(res["effects"])
	if len(toAnySlice(effects["toasts"])) != 0 {
		t.Fatalf("static inline regions were called toasts: %+v", effects)
	}
	regions := fmt.Sprint(effects["live_regions"])
	if !strings.Contains(regions, "Membership active") || !strings.Contains(regions, "Required field missing") || strings.Contains(regions, "Route changed") {
		t.Fatalf("wrong inline region digest: %+v", effects)
	}
	for _, f := range p.engine.FactsByPredicate("toast_notification") {
		if strings.Contains(fmt.Sprint(f.Args), "Membership active") || strings.Contains(fmt.Sprint(f.Args), "Required field missing") {
			t.Fatalf("inline region became toast fact: %+v", f)
		}
	}
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "click", "ref": "noop"})})
	if effects := asMap(res["effects"]); effects["live_regions"] != nil || effects["toasts"] != nil {
		t.Fatalf("unchanged regions replayed: %+v", effects)
	}
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "click", "ref": "inline"})})
	effects = asMap(res["effects"])
	if !strings.Contains(fmt.Sprint(effects["live_regions"]), "Account input invalid") || effects["toasts"] != nil {
		t.Fatalf("dynamic inline alert mislabeled: %+v", effects)
	}
	if len(p.engine.FactsByPredicate("live_region_notification")) < 3 {
		t.Fatal("typed live region events missing")
	}
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "click", "ref": "update"})})
	if effects := asMap(res["effects"]); !strings.Contains(fmt.Sprint(effects["live_regions"]), "Email address invalid") || effects["toasts"] != nil {
		t.Fatalf("reused inline region text was lost or called a toast: %+v", effects)
	}
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "click", "ref": "toast"})})
	effects = asMap(res["effects"])
	if !strings.Contains(fmt.Sprint(effects["toasts"]), "Settings saved") || effects["live_regions"] != nil {
		t.Fatalf("real toast lost or duplicated into live regions: %+v", effects)
	}
	errors, err := p.engine.Query(p.ctx, `user_visible_error(S, "live_region", "Account input invalid", T).`)
	if err != nil || len(errors) == 0 {
		t.Fatalf("inline error lost from unified evidence: %v %v", errors, err)
	}
	pageErrors, err := p.engine.Query(p.ctx, `page_has_error(S, "Account input invalid").`)
	if err != nil || len(pageErrors) == 0 {
		t.Fatalf("inline error lost from page state: %v %v", pageErrors, err)
	}
	toastCount := 0
	for _, f := range p.engine.FactsByPredicate("toast_notification") {
		if strings.Contains(fmt.Sprint(f.Args), "Settings saved") {
			toastCount++
		}
	}
	if toastCount != 1 {
		t.Fatalf("nested real toast emitted %d facts, want one", toastCount)
	}
	for _, f := range p.engine.FactsByPredicate("live_region_notification") {
		if strings.Contains(fmt.Sprint(f.Args), "Route changed") || strings.Contains(fmt.Sprint(f.Args), "Settings saved") {
			t.Fatalf("route announcement or toast leaked to live region facts: %+v", f)
		}
	}
	ev := collectBrowserEvidence(p.ctx, p.engine, reasonScope{sessionID: p.session})
	d := diagnose(ev)
	if d.score.toastErrors != 0 || d.score.liveRegionErrors < 2 || d.section("live_regions", 20) == nil {
		t.Fatalf("diagnosis lost inline errors or called them toasts: %+v", d.score.row())
	}
	for _, issue := range d.issues {
		if issue.kind == "toast_error" {
			t.Fatalf("inline error became toast diagnosis: %+v", issue)
		}
	}
}
