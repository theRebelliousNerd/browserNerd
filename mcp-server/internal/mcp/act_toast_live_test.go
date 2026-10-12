package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A failed login on testdata/probe/toast.html raises one sonner toast, its
// description rendered a frame after its title, and the form's inline alert
// repeats the description. The act result names it once, with the title and
// description apart, and the toast facts carry the separated text.
func TestLiveOneFailedLoginIsOneToast(t *testing.T) {
	p := startLiveProbe(t)
	p.actOK(t, map[string]interface{}{
		"operations": ops(map[string]interface{}{"type": "navigate", "url": p.baseURL + "/toast.html"}),
	})
	res := p.actOK(t, map[string]interface{}{
		"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Sign in"}),
	})
	effects, _ := res["effects"].(map[string]interface{})
	toasts, _ := effects["toasts"].([]string)
	raw, _ := json.Marshal(effects)
	if len(toasts) != 1 || toasts[0] != "error: Login failed Invalid credentials" {
		t.Fatalf("one failed login is one toast, spelled with its words apart; effects %s", raw)
	}

	var texts []string
	for _, f := range p.engine.FactsByPredicate("toast_notification") {
		if len(f.Args) >= 2 {
			texts = append(texts, fmt.Sprint(f.Args[1]))
		}
	}
	joined := strings.Join(texts, " | ")
	if strings.Contains(joined, "failedInvalid") {
		t.Fatalf("a toast fact ran the title into the description: %s", joined)
	}
	if !strings.Contains(joined, "Login failed Invalid credentials") {
		t.Fatalf("the toast facts should carry the whole notification: %s", joined)
	}
	for _, text := range texts {
		if text == "Invalid credentials" {
			t.Fatalf("the description alone is a part of a toast already reported, not a second toast: %s", joined)
		}
	}
}
