package mcp

import "testing"

func TestReasonNavigationDocumentIdentity(t *testing.T) {
	for _, mode := range []string{"rapid_reload", "long_load", "redirect", "same_url_fetch", "iframe", "same_document", "hash_route", "query_route", "other_session", "missing_request"} {
		t.Run(mode, func(t *testing.T) {
			engine := testMangleEngineForProgressive(t)
			p := newProbeFacts("navigation-identity")
			url := probeOrigin + "/records?view=all"
			p.request("old-doc", "GET", url, 100, 200, 1)
			p.add("net_request_context", 100, "old-doc", "Document", "main", "old-loader")
			p.add("navigation_event", 120, url, p.at(120).UnixMilli())
			p.add("navigation_context", 120, url, "document", "old-loader", p.at(120).UnixMilli())
			navMs, requestMs := int64(1000), int64(900)
			if mode == "long_load" {
				navMs = 35000
			}
			if mode != "missing_request" {
				p.request("new-doc", "GET", url, requestMs, 200, 1)
				p.add("net_request_context", requestMs, "new-doc", "Document", "main", "new-loader")
			}
			p.add("navigation_event", navMs, url, p.at(navMs).UnixMilli())
			p.add("navigation_context", navMs, url, "document", "new-loader", p.at(navMs).UnixMilli())
			// Structured navigate also confirms the final URL after commit.
			p.add("navigation_event", navMs+10, url, p.at(navMs+10).UnixMilli())
			want := p.at(requestMs).UnixMilli()
			switch mode {
			case "redirect":
				p.request("new-doc", "GET", probeOrigin+"/entry", 700, 302, 1)
				want = p.at(700).UnixMilli()
			case "same_url_fetch", "iframe":
				kind, loader := "Fetch", "new-loader"
				if mode == "iframe" {
					kind, loader = "Document", "iframe-loader"
				}
				p.request("unrelated", "GET", url, navMs-1, 200, 1)
				p.add("net_request_context", navMs-1, "unrelated", kind, "child", loader)
			case "same_document", "hash_route", "query_route":
				if mode == "hash_route" {
					url += "#selected"
				} else if mode == "query_route" {
					url = probeOrigin + "/records?view=selected"
				}
				p.request("route-fetch", "GET", url, navMs+20, 200, 1)
				p.add("net_request_context", navMs+20, "route-fetch", "Fetch", "main", "new-loader")
				p.add("navigation_event", navMs+30, url, p.at(navMs+30).UnixMilli())
				p.add("navigation_context", navMs+30, url, "same_document", "", p.at(navMs+30).UnixMilli())
				want = p.at(navMs + 30).UnixMilli()
			case "other_session":
				other := newProbeFacts("other-session")
				other.base = p.base
				other.request("new-doc", "GET", url, navMs-1, 200, 1)
				other.add("net_request_context", navMs-1, "new-doc", "Document", "main", "new-loader")
				loadFacts(t, engine, other)
			case "missing_request":
				want = p.at(navMs + 10).UnixMilli()
			}
			loadFacts(t, engine, p)
			if got := currentPageStartMs(engine, p.sid); got != want {
				t.Fatalf("document boundary = %d, want %d", got, want)
			}
		})
	}
}
