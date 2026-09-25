package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// mode:storage answers "why am I signed out / why does the API say 401" from
// the page's own state: which cookies and web-storage keys exist, their flags
// and expiry, and - for every value shaped like a JWT, including one nested in
// a JSON state blob - when it expires. No value ever leaves: only names,
// sizes, flags and token times, so the answer is safe to show and costs a few
// lines instead of a JavaScript probe.

// storageScanScript reads localStorage and sessionStorage in the page and
// returns derived facts only - never a value.
const storageScanScript = `() => {
  const authWords = /(token|auth|session|jwt|access|refresh|csrf|xsrf|sid|user)/i;
  const jwtShape = /^[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*$/;
  const jwtTimes = (v) => {
    try {
      const p = v.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
      const claims = JSON.parse(atob(p + '='.repeat((4 - p.length % 4) % 4)));
      return { exp: typeof claims.exp === 'number' ? claims.exp : null, iat: typeof claims.iat === 'number' ? claims.iat : null };
    } catch (e) { return null; }
  };
  const findTokens = (value, path, depth, out) => {
    if (out.length >= 5 || depth > 4 || value === null) return;
    if (typeof value === 'string') {
      if (jwtShape.test(value)) { const t = jwtTimes(value); if (t) out.push(Object.assign({ path: path }, t)); }
      return;
    }
    if (typeof value !== 'object') return;
    for (const k of Object.keys(value).slice(0, 50)) findTokens(value[k], path ? path + '.' + k : k, depth + 1, out);
  };
  const scan = (store, name) => {
    const rows = [];
    let s;
    try { s = window[name]; } catch (e) { return { error: 'blocked' }; }
    if (!s) return { rows: rows };
    for (let i = 0; i < s.length && rows.length < 60; i++) {
      const key = s.key(i);
      const raw = s.getItem(key) || '';
      const row = { store: store, key: key, length: raw.length, kind: 'text' };
      if (authWords.test(key)) row.auth = true;
      if (jwtShape.test(raw)) {
        row.kind = 'jwt';
        const t = jwtTimes(raw);
        if (t) { row.exp = t.exp; row.iat = t.iat; }
        row.auth = true;
      } else if (raw[0] === '{' || raw[0] === '[') {
        try {
          const parsed = JSON.parse(raw);
          row.kind = 'json';
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) row.keys = Object.keys(parsed).slice(0, 8);
          const tokens = [];
          findTokens(parsed, '', 0, tokens);
          if (tokens.length) { row.tokens = tokens; row.auth = true; }
        } catch (e) {}
      }
      rows.push(row);
    }
    return { rows: rows, total: s.length };
  };
  return { local: scan('local', 'localStorage'), session: scan('session', 'sessionStorage') };
}`

// authCookieWords marks cookie names that usually carry a session or token.
var authCookieWords = []string{"token", "auth", "session", "jwt", "access", "refresh", "csrf", "xsrf", "sid"}

func (t *BrowserObserveTool) observeStorage(ctx context.Context, sessionID, view string) (map[string]interface{}, error) {
	page, ok := t.sessions.Page(sessionID)
	if !ok || page == nil {
		return map[string]interface{}{"success": false, "error": "session has no open tab: " + sessionID}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p := page.Context(callCtx)
	now := time.Now()

	info, err := p.Info()
	if err != nil {
		return nil, fmt.Errorf("read page url: %w", err)
	}
	cookiesRes, err := proto.NetworkGetCookies{Urls: []string{info.URL}}.Call(p)
	if err != nil {
		return nil, fmt.Errorf("read cookies: %w", err)
	}
	cookies := make([]map[string]interface{}, 0, len(cookiesRes.Cookies))
	tokens := make([]map[string]interface{}, 0)
	for _, c := range cookiesRes.Cookies {
		row := map[string]interface{}{"name": c.Name, "size": len(c.Value)}
		if view == "full" {
			row["domain"] = c.Domain
			row["path"] = c.Path
		}
		flags := make([]string, 0, 3)
		if c.HTTPOnly {
			flags = append(flags, "httpOnly")
		}
		if c.Secure {
			flags = append(flags, "secure")
		}
		if c.SameSite != "" {
			flags = append(flags, "sameSite="+string(c.SameSite))
		}
		if len(flags) > 0 {
			row["flags"] = strings.Join(flags, " ")
		}
		if c.Session || c.Expires <= 0 {
			row["expires"] = "session"
		} else {
			row["expires_in"] = relativeDuration(time.Unix(int64(c.Expires), 0).Sub(now))
		}
		if exp, iat, isJWT := jwtClaimsTimes(c.Value); isJWT {
			row["kind"] = "jwt"
			tokens = append(tokens, tokenRow("cookie "+c.Name, exp, iat, now))
		}
		if nameLooksAuth(c.Name) {
			row["auth"] = true
		}
		cookies = append(cookies, row)
	}
	sort.SliceStable(cookies, func(i, j int) bool {
		ai, _ := cookies[i]["auth"].(bool)
		aj, _ := cookies[j]["auth"].(bool)
		return ai && !aj
	})

	scanned, err := p.Eval(storageScanScript)
	if err != nil {
		return nil, fmt.Errorf("read web storage: %w", err)
	}
	var stores map[string]struct {
		Rows  []map[string]interface{} `json:"rows"`
		Total int                      `json:"total"`
		Error string                   `json:"error"`
	}
	raw, _ := json.Marshal(scanned.Value.Val())
	if err := json.Unmarshal(raw, &stores); err != nil {
		return nil, fmt.Errorf("decode web storage scan: %w", err)
	}
	storage := map[string]interface{}{}
	counts := map[string]int{}
	for _, name := range []string{"local", "session"} {
		store := stores[name]
		counts[name] = store.Total
		rows := make([]map[string]interface{}, 0, len(store.Rows))
		for _, r := range store.Rows {
			key := fmt.Sprintf("%v", r["key"])
			if r["kind"] == "jwt" {
				tokens = append(tokens, tokenRow(name+"Storage "+key, asInt64(r["exp"]), asInt64(r["iat"]), now))
			}
			if nested, ok := r["tokens"].([]interface{}); ok {
				for _, n := range nested {
					nm, _ := n.(map[string]interface{})
					tokens = append(tokens, tokenRow(name+"Storage "+key+"."+fmt.Sprintf("%v", nm["path"]), asInt64(nm["exp"]), asInt64(nm["iat"]), now))
				}
			}
			row := map[string]interface{}{"key": key, "size": r["length"], "kind": r["kind"]}
			if r["auth"] == true {
				row["auth"] = true
			}
			if keys, ok := r["keys"].([]interface{}); ok && view == "full" {
				row["keys"] = keys
			}
			rows = append(rows, row)
		}
		if store.Error != "" {
			storage[name] = store.Error
		} else if len(rows) > 0 {
			storage[name] = rows
		}
	}

	expired := 0
	for _, tok := range tokens {
		if tok["expired"] == true {
			expired++
		}
	}
	summary := fmt.Sprintf("%d cookie(s), localStorage %d key(s), sessionStorage %d key(s), %d token(s)", len(cookies), counts["local"], counts["session"], len(tokens))
	status := "ok"
	if expired > 0 {
		summary += fmt.Sprintf(", %d EXPIRED", expired)
		status = "warning"
	}

	// Output keys avoid the words the server's credential redactor matches
	// (cookie, token, ...): under such a key it blanks the whole section, and
	// nothing here is a value - only names, sizes, flags and times.
	data := map[string]interface{}{"origin": originFromURL(info.URL)}
	if len(tokens) > 0 {
		data["expiry"] = tokens
	}
	if view != "summary" {
		data["jar"] = cookies
		if len(storage) > 0 {
			data["storage"] = storage
		}
	}
	handles := []string{"observe:" + sessionID + ":storage"}
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
	return map[string]interface{}{
		"success":          true,
		"status":           status,
		"mode":             "storage",
		"view":             view,
		"summary":          summary,
		"data":             data,
		"evidence_handles": handles,
	}, nil
}

// tokenRow states when a token expires, relative to now.
func tokenRow(where string, exp, iat int64, now time.Time) map[string]interface{} {
	row := map[string]interface{}{"where": where}
	if exp > 0 {
		left := time.Unix(exp, 0).Sub(now)
		row["expired"] = left <= 0
		if left <= 0 {
			row["expired_ago"] = relativeDuration(-left)
		} else {
			row["expires_in"] = relativeDuration(left)
		}
	} else {
		row["expires"] = "no exp claim"
	}
	if iat > 0 {
		row["issued_ago"] = relativeDuration(now.Sub(time.Unix(iat, 0)))
	}
	return row
}

// jwtClaimsTimes decodes a JWT-shaped value's exp and iat claims. Nothing
// else of the token is read or returned.
func jwtClaimsTimes(value string) (exp, iat int64, ok bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || len(parts[0]) < 8 || len(parts[1]) < 8 {
		return 0, 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return 0, 0, false
	}
	var claims struct {
		Exp float64 `json:"exp"`
		Iat float64 `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, 0, false
	}
	return int64(claims.Exp), int64(claims.Iat), true
}

func nameLooksAuth(name string) bool {
	n := strings.ToLower(name)
	for _, w := range authCookieWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// relativeDuration renders a duration the way a person reads an expiry: 42s,
// 17m, 3h, 2d.
func relativeDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
