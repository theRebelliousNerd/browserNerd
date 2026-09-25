package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testJWT(t *testing.T, exp time.Time, marker string) string {
	t.Helper()
	enc := func(v interface{}) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	header := enc(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload := enc(map[string]interface{}{"sub": marker, "exp": exp.Unix(), "iat": exp.Add(-time.Hour).Unix()})
	return header + "." + payload + ".c2lnbmF0dXJlLXNlY3JldA"
}

// TestLiveStorageNamesAnExpiredTokenWithoutRevealingIt proves mode:storage on
// a real page: a valid token in a cookie, an expired one nested in a
// zustand-style JSON blob in localStorage, and not one value in the answer.
func TestLiveStorageNamesAnExpiredTokenWithoutRevealingIt(t *testing.T) {
	p := startLiveProbe(t)
	page, _ := p.manager.Page(p.session)

	valid := testJWT(t, time.Now().Add(40*time.Minute), "user-valid-marker")
	expired := testJWT(t, time.Now().Add(-3*time.Hour), "user-expired-marker")
	setup := fmt.Sprintf(`() => {
		document.cookie = "access_token=%s; path=/";
		document.cookie = "theme=dark; path=/";
		localStorage.setItem("auth-storage", JSON.stringify({state: {token: %q, user: {email: "secret-email-marker"}}, version: 0}));
		localStorage.setItem("sidebar", "collapsed");
		sessionStorage.setItem("draft", "private-draft-marker");
	}`, valid, expired)
	if _, err := page.Eval(setup); err != nil {
		t.Fatal(err)
	}

	observe := &BrowserObserveTool{sessions: p.manager, engine: p.engine}
	res, err := observe.Execute(p.ctx, map[string]interface{}{"session_id": p.session, "mode": "storage"})
	if err != nil {
		t.Fatal(err)
	}
	// Read it as an agent does: after the server's credential redactor, which
	// blanks any section under a key that names a cookie or a token.
	out := throughServerRedactor(t, "browser-observe", res)
	raw, _ := json.Marshal(out)
	body := string(raw)
	t.Logf("storage compact: ~%d tokens: %s", len(raw)/4, out["summary"])

	for _, secret := range []string{valid, expired, "user-valid-marker", "user-expired-marker", "secret-email-marker", "private-draft-marker", "c2lnbmF0dXJl", "dark", "collapsed"} {
		if strings.Contains(body, secret) {
			t.Fatalf("storage mode leaked a value (%q): %s", secret, body)
		}
	}
	if !strings.Contains(fmt.Sprint(out["summary"]), "1 EXPIRED") || out["status"] != "warning" {
		t.Fatalf("summary must name one expired token: %v / %v", out["summary"], out["status"])
	}
	data := out["data"].(map[string]interface{})
	if jar, ok := data["jar"].([]interface{}); !ok || len(jar) != 2 {
		t.Fatalf("the cookie list must survive the redactor: %v", data["jar"])
	}
	tokens := toMapSlice(data["expiry"])
	var sawCookie, sawNested bool
	for _, tok := range tokens {
		switch tok["where"] {
		case "cookie access_token":
			sawCookie = tok["expired"] == false && tok["expires_in"] != nil
		case "localStorage auth-storage.state.token":
			sawNested = tok["expired"] == true && tok["expired_ago"] == "3h"
		}
	}
	if !sawCookie || !sawNested {
		t.Fatalf("want a live cookie token and an expired nested localStorage token, got %v", tokens)
	}
	if len(raw)/4 > 400 {
		t.Fatalf("storage compact cost ~%d tokens, budget 400", len(raw)/4)
	}
}

func TestJWTClaimsTimesReadsOnlyTimes(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	gotExp, gotIat, ok := jwtClaimsTimes(testJWT(t, exp, "x"))
	if !ok || gotExp != exp.Unix() || gotIat != exp.Add(-time.Hour).Unix() {
		t.Fatalf("exp=%d iat=%d ok=%v", gotExp, gotIat, ok)
	}
	for _, notJWT := range []string{"", "abc", "a.b.c", "dark", "eyJhbGciOi.not-base64!.x"} {
		if _, _, ok := jwtClaimsTimes(notJWT); ok {
			t.Fatalf("%q read as a JWT", notJWT)
		}
	}
}
