package browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/config"
)

func TestTraceRepositoryWorkspaceCorrelatesFrontendAndBackend(t *testing.T) {
	tmpDir := t.TempDir()

	frontendDir := filepath.Join(tmpDir, "frontend", "src")
	backendDir := filepath.Join(tmpDir, "backend", "src")
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatalf("failed to create frontend dir: %v", err)
	}
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatalf("failed to create backend dir: %v", err)
	}

	frontendPage := "export async function updateOrder(orderId, token) {\n" +
		"  return fetch(\"/api/orders/\" + orderId, {\n" +
		"    method: \"PATCH\",\n" +
		"    headers: { Authorization: \"Bearer \" + token },\n" +
		"    body: JSON.stringify({ status: \"approved\", note: \"ship it\" }),\n" +
		"  });\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(frontendDir, "OrdersPage.tsx"), []byte(frontendPage), 0o644); err != nil {
		t.Fatalf("failed to write frontend page: %v", err)
	}

	frontendClient := `export function createOrder(payload) {
  return axios.post("/api/orders", payload);
}
`
	if err := os.WriteFile(filepath.Join(frontendDir, "orderClient.ts"), []byte(frontendClient), 0o644); err != nil {
		t.Fatalf("failed to write frontend client: %v", err)
	}

	backendRoutes := `router.patch("/api/orders/:orderId", requireAuth, validateBody(orderPatchSchema), updateOrder)
router.post("/api/orders", requireAuth, validateBody(createOrderSchema), createOrder)
`
	if err := os.WriteFile(filepath.Join(backendDir, "orders.routes.ts"), []byte(backendRoutes), 0o644); err != nil {
		t.Fatalf("failed to write backend routes: %v", err)
	}

	traceCfg := resolveRepoTraceConfig(config.RepoTraceConfig{
		Enabled:            true,
		RootDir:            tmpDir,
		SearchRoots:        []string{"frontend", "backend"},
		MaxFiles:           100,
		MaxFileBytes:       64 * 1024,
		MaxSeedHints:       16,
		MaxNavigationHints: 8,
		MaxControlHints:    8,
		MaxPlanSteps:       12,
		MaxFrontendMatches: 10,
		MaxBackendMatches:  10,
	}, nil)

	report, err := traceRepositoryWorkspace(context.Background(), "session-123", RepoTraceBrowserContext{
		CurrentURL:     "https://app.example.com/orders/42",
		CurrentPath:    "/orders/42",
		Title:          "Orders Dashboard",
		ComponentNames: []string{"OrdersPage"},
		Forms: []RepoTraceFormHint{{
			Method: "PATCH",
			Action: "/api/orders/42",
			Fields: []string{"status", "note"},
		}},
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Orders", Href: "/admin/orders", Region: "nav"},
			{Label: "Release Notes", Href: "https://docs.example.com/orders", Region: "footer"},
		},
		Controls: []RepoTraceControlHint{
			{Label: "Delete Order", TagName: "button", ControlType: "button", FormAction: "/api/orders/42", FormMethod: "DELETE"},
			{Label: "Password", TagName: "input", ControlType: "password", Name: "password", Autocomplete: "current-password"},
		},
		Requests: []RepoTraceRequestSeed{
			{Method: "PATCH", URL: "https://app.example.com/api/orders/42", Path: "/api/orders/42"},
			{Method: "POST", URL: "https://app.example.com/api/orders", Path: "/api/orders"},
		},
	}, traceCfg)
	if err != nil {
		t.Fatalf("traceRepositoryWorkspace returned error: %v", err)
	}

	if len(report.FrontendSites) < 2 {
		t.Fatalf("expected at least 2 frontend matches, got %d", len(report.FrontendSites))
	}
	if len(report.BackendMatches) < 2 {
		t.Fatalf("expected at least 2 backend matches, got %d", len(report.BackendMatches))
	}
	if len(report.Correlations) == 0 {
		t.Fatal("expected at least one frontend/backend correlation")
	}
	if report.HazardSummary.WriteCapableForms != 1 {
		t.Fatalf("expected one write-capable form, got %+v", report.HazardSummary)
	}
	if report.HazardSummary.DestructiveControls != 1 {
		t.Fatalf("expected one destructive control, got %+v", report.HazardSummary)
	}
	if report.HazardSummary.AuthSensitiveControls != 1 {
		t.Fatalf("expected one auth-sensitive control, got %+v", report.HazardSummary)
	}
	if report.HazardSummary.AuthSensitiveNavigationLinks != 1 {
		t.Fatalf("expected one auth-sensitive navigation link, got %+v", report.HazardSummary)
	}
	if report.HazardSummary.InternalNavigationLinks != 1 {
		t.Fatalf("expected one internal navigation link candidate, got %+v", report.HazardSummary)
	}
	if len(report.AuditPlan) == 0 {
		t.Fatal("expected deterministic audit plan steps")
	}

	var foundPatchFrontend bool
	for _, site := range report.FrontendSites {
		if (site.Endpoint == "/api/orders/:param" || site.Endpoint == "/api/orders") && site.Method == "PATCH" {
			foundPatchFrontend = true
			if !strings.HasPrefix(site.EvidenceHandle, "trace:") {
				t.Fatalf("expected trace evidence handle, got %q", site.EvidenceHandle)
			}
		}
	}
	if !foundPatchFrontend {
		t.Fatalf("expected PATCH frontend request site in %+v", report.FrontendSites)
	}

	var foundBackendRoute bool
	for _, route := range report.BackendMatches {
		if route.RoutePath == "/api/orders/:orderid" || route.RoutePath == "/api/orders/:orderId" {
			foundBackendRoute = true
			if !strings.Contains(route.AuthExpectation, "requireAuth") {
				t.Fatalf("expected auth expectation to contain requireAuth, got %q", route.AuthExpectation)
			}
			if !strings.Contains(route.PayloadExpectation, "orderPatchSchema") {
				t.Fatalf("expected payload expectation to contain orderPatchSchema, got %q", route.PayloadExpectation)
			}
		}
	}
	if !foundBackendRoute {
		t.Fatalf("expected backend PATCH route in %+v", report.BackendMatches)
	}

	var sawNavSeed, sawControlSeed, sawDestructiveStep bool
	for _, seed := range report.Seeds {
		switch seed.Kind {
		case "nav_path":
			sawNavSeed = true
		case "control_path":
			sawControlSeed = true
		}
	}
	for _, step := range report.AuditPlan {
		if step.Kind == "review_control" && strings.Contains(strings.Join(step.HazardTypes, ","), "likely_destructive") {
			sawDestructiveStep = true
		}
	}
	if !sawNavSeed || !sawControlSeed {
		t.Fatalf("expected audit discovery seeds, got %+v", report.Seeds)
	}
	if !sawDestructiveStep {
		t.Fatalf("expected destructive review step in %+v", report.AuditPlan)
	}

	var sawFrontendFact, sawBackendFact bool
	for _, fact := range report.Facts {
		switch fact.Predicate {
		case "repo_trace_frontend_request_site":
			sawFrontendFact = true
		case "repo_trace_backend_expectation":
			sawBackendFact = true
		}
	}
	if !sawFrontendFact || !sawBackendFact {
		t.Fatalf("expected frontend/backend repo trace facts, got predicates %+v", report.Facts)
	}
}

func TestNormalizeRepoTraceBrowserContextClassifiesAuditDiscoveryHints(t *testing.T) {
	ctx := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		Forms: []RepoTraceFormHint{{
			Method:       "post",
			Action:       "https://app.example.com/api/session/reset-password",
			Fields:       []string{"email", "password", "otp"},
			SubmitLabels: []string{"Reset Password"},
		}},
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
			{Label: "Billing Docs", Href: "https://docs.example.com/billing", Region: "footer"},
			{Label: "Jump to Logs", Href: "#logs", Region: "nav"},
		},
		Controls: []RepoTraceControlHint{
			{Label: "Delete Account", TagName: "button", ControlType: "button", FormAction: "/api/account/delete", FormMethod: "DELETE"},
			{Label: "Password", TagName: "input", ControlType: "password", Name: "password", Autocomplete: "current-password"},
		},
	}, 12, 8, 8)

	if len(ctx.Forms) != 1 || !ctx.Forms[0].WriteCapable || !ctx.Forms[0].AuthSensitive {
		t.Fatalf("expected POST reset-password form to be write-capable and auth-sensitive, got %+v", ctx.Forms)
	}
	if len(ctx.NavigationLinks) != 3 {
		t.Fatalf("expected 3 navigation links, got %+v", ctx.NavigationLinks)
	}
	if ctx.NavigationLinks[0].LinkType != "internal" || !ctx.NavigationLinks[0].AuthSensitive {
		t.Fatalf("expected internal admin link to be auth-sensitive, got %+v", ctx.NavigationLinks[0])
	}
	if ctx.NavigationLinks[1].LinkType != "external" {
		t.Fatalf("expected external docs link, got %+v", ctx.NavigationLinks[1])
	}
	if ctx.NavigationLinks[2].LinkType != "hash" {
		t.Fatalf("expected hash link, got %+v", ctx.NavigationLinks[2])
	}
	if len(ctx.Controls) != 2 {
		t.Fatalf("expected 2 controls, got %+v", ctx.Controls)
	}
	if !ctx.Controls[0].LikelyDestructive || !ctx.Controls[0].WriteCapable {
		t.Fatalf("expected delete control to be destructive write surface, got %+v", ctx.Controls[0])
	}
	if !ctx.Controls[1].AuthSensitive {
		t.Fatalf("expected password control to be auth-sensitive, got %+v", ctx.Controls[1])
	}
}

func TestNormalizeRepoTraceBrowserContextDistinguishesRevealAndInternalNavigationControls(t *testing.T) {
	ctx := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
			{Label: "Current Page", Href: "/settings/security", Region: "nav"},
			{Label: "Sign Out", Href: "/logout", Region: "nav"},
		},
		Controls: []RepoTraceControlHint{
			{Label: "Show Filters", TagName: "button", ControlType: "button", Region: "sidebar", AriaControls: "filters-panel", AriaExpanded: "false"},
			{Label: "Billing", TagName: "button", ControlType: "button", Region: "nav", Href: "/settings/billing"},
			{Label: "Save Changes", TagName: "button", ControlType: "submit", FormAction: "/api/settings/security", FormMethod: "POST"},
			{Label: "Delete Account", TagName: "button", ControlType: "button", FormAction: "/api/account/delete", FormMethod: "DELETE"},
		},
	}, 12, 8, 12)

	if !ctx.NavigationLinks[0].InternalCandidate {
		t.Fatalf("expected admin link to be an internal navigation candidate, got %+v", ctx.NavigationLinks[0])
	}
	if ctx.NavigationLinks[1].InternalCandidate {
		t.Fatalf("expected current-page link to be skipped as a discovery candidate, got %+v", ctx.NavigationLinks[1])
	}
	if ctx.NavigationLinks[2].InternalCandidate {
		t.Fatalf("expected sign-out link to be excluded from recursive navigation, got %+v", ctx.NavigationLinks[2])
	}
	if !ctx.Controls[0].RevealSurface || ctx.Controls[0].RevealKind != "disclosure" || ctx.Controls[0].WriteCapable {
		t.Fatalf("expected show-filters control to be a non-write reveal surface, got %+v", ctx.Controls[0])
	}
	if !ctx.Controls[1].InternalNavigation || ctx.Controls[1].WriteCapable {
		t.Fatalf("expected billing button to be an internal navigation control, got %+v", ctx.Controls[1])
	}
	if !ctx.Controls[2].WriteCapable || ctx.Controls[2].InternalNavigation || ctx.Controls[2].RevealSurface {
		t.Fatalf("expected save button to remain write-capable only, got %+v", ctx.Controls[2])
	}
	if !ctx.Controls[3].LikelyDestructive || !ctx.Controls[3].WriteCapable {
		t.Fatalf("expected delete button to remain destructive, got %+v", ctx.Controls[3])
	}

	summary := summarizeRepoTraceHazards(ctx)
	if summary.InternalNavigationLinks != 1 || summary.InternalNavigationControls != 1 || summary.RevealControls != 1 {
		t.Fatalf("expected discovery counts for link/control/reveal surfaces, got %+v", summary)
	}
}

func TestBuildRepoTraceAuditPlanPrioritizesRevealAndInternalNavigationCandidates(t *testing.T) {
	ctx := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
			{Label: "Docs", Href: "https://docs.example.com/security", Region: "footer"},
		},
		Controls: []RepoTraceControlHint{
			{Label: "Show Filters", TagName: "button", ControlType: "button", Region: "sidebar", AriaControls: "filters-panel", AriaExpanded: "false"},
			{Label: "Billing", TagName: "button", ControlType: "button", Region: "nav", Href: "/settings/billing"},
			{Label: "Delete Account", TagName: "button", ControlType: "button", FormAction: "/api/account/delete", FormMethod: "DELETE"},
		},
		Forms: []RepoTraceFormHint{{
			Method:       "post",
			Action:       "/api/session/reset-password",
			Fields:       []string{"password", "otp"},
			SubmitLabels: []string{"Reset Password"},
		}},
	}, 12, 8, 12)

	plan := buildRepoTraceAuditPlan(ctx, 10)
	if len(plan) < 5 {
		t.Fatalf("expected at least 5 plan steps, got %+v", plan)
	}
	if plan[0].Kind != "capture_context" {
		t.Fatalf("expected capture_context first, got %+v", plan[0])
	}
	if plan[1].Kind != "reveal_surface" || plan[1].Mode != "reveal_only" {
		t.Fatalf("expected reveal surface to be prioritized second, got %+v", plan[1])
	}

	var sawNavLink, sawNavControl, sawDeleteReview bool
	for _, step := range plan {
		switch {
		case step.Kind == "map_navigation" && step.Source == "browser.navigation_link":
			sawNavLink = true
		case step.Kind == "map_navigation" && step.Source == "browser.control":
			sawNavControl = true
		case step.Kind == "review_control" && strings.Contains(strings.Join(step.HazardTypes, ","), "likely_destructive"):
			sawDeleteReview = true
		}
	}
	if !sawNavLink || !sawNavControl || !sawDeleteReview {
		t.Fatalf("expected reveal/nav/destructive plan coverage, got %+v", plan)
	}
	for _, step := range plan {
		if step.Target == "Docs" {
			t.Fatalf("expected external docs link to stay out of recursive navigation plan, got %+v", plan)
		}
	}
}

func TestBuildRepoTraceContextRefreshAppendsOnlyNewSurfaces(t *testing.T) {
	before := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		Title:       "Security Settings",
		Headings:    []string{"Security"},
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
		},
		Controls: []RepoTraceControlHint{
			{Label: "Show Filters", TagName: "button", ControlType: "button", Region: "sidebar", AriaControls: "filters-panel", AriaExpanded: "false"},
			{Label: "Billing", TagName: "button", ControlType: "button", Region: "nav", Href: "/settings/billing"},
		},
	}, 12, 8, 12)

	after := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security/advanced",
		CurrentPath: "/settings/security/advanced",
		Title:       "Advanced Security Settings",
		Headings:    []string{"Security", "Advanced"},
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
			{Label: "API Keys", Href: "/settings/api-keys", Region: "nav"},
			{Label: "Docs", Href: "https://docs.example.com/security", Region: "footer"},
		},
		Forms: []RepoTraceFormHint{{
			Method:       "post",
			Action:       "/api/settings/security/challenge",
			Fields:       []string{"challenge_code"},
			SubmitLabels: []string{"Verify Challenge"},
		}},
		Controls: []RepoTraceControlHint{
			{Label: "Show Filters", TagName: "button", ControlType: "button", Region: "sidebar", AriaControls: "filters-panel", AriaExpanded: "true"},
			{Label: "Billing", TagName: "button", ControlType: "button", Region: "nav", Href: "/settings/billing"},
			{Label: "Open Session History", TagName: "button", ControlType: "button", Region: "main", AriaControls: "session-history", AriaExpanded: "false"},
			{Label: "Delete Recovery Key", TagName: "button", ControlType: "button", FormAction: "/api/settings/security/recovery-key", FormMethod: "DELETE"},
		},
	}, 12, 8, 12)

	refresh := buildRepoTraceContextRefresh(before, after, 8)
	if !refresh.Changed {
		t.Fatal("expected refresh to detect new discovery surfaces")
	}
	if !refresh.RouteChanged || !refresh.SnapshotChanged {
		t.Fatalf("expected route and snapshot changes, got %+v", refresh)
	}
	if len(refresh.NewNavigationLinks) != 2 {
		t.Fatalf("expected two new links (internal + external), got %+v", refresh.NewNavigationLinks)
	}
	if len(refresh.NewForms) != 1 {
		t.Fatalf("expected one newly exposed form, got %+v", refresh.NewForms)
	}
	if len(refresh.NewControls) != 2 {
		t.Fatalf("expected two newly exposed controls, got %+v", refresh.NewControls)
	}
	if !refresh.PlanExpanded {
		t.Fatalf("expected appended plan expansion, got %+v", refresh)
	}
	if len(refresh.AppendedPlan) == 0 || refresh.AppendedPlan[0].Kind != "capture_context" {
		t.Fatalf("expected capture_context to lead appended plan, got %+v", refresh.AppendedPlan)
	}

	var sawReveal, sawNav, sawForm, sawDelete bool
	for _, step := range refresh.AppendedPlan {
		switch {
		case step.Kind == "reveal_surface":
			sawReveal = true
		case step.Kind == "map_navigation" && step.Path == "/settings/api-keys":
			sawNav = true
		case step.Kind == "inspect_form" && step.Path == "/api/settings/security/challenge":
			sawForm = true
		case step.Kind == "review_control" && strings.Contains(strings.Join(step.HazardTypes, ","), "likely_destructive"):
			sawDelete = true
		case step.Target == "Docs":
			t.Fatalf("expected external docs link to stay out of appended plan, got %+v", refresh.AppendedPlan)
		}
	}
	if !sawReveal || !sawNav || !sawForm || !sawDelete {
		t.Fatalf("expected appended plan to cover reveal/navigation/form/destructive control, got %+v", refresh.AppendedPlan)
	}
}

func TestBuildRepoTraceContextRefreshIgnoresReorderedExistingSurfaces(t *testing.T) {
	before := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		Title:       "Security Settings",
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
			{Label: "Billing", Href: "/settings/billing", Region: "nav"},
		},
		Forms: []RepoTraceFormHint{{
			Method:       "post",
			Action:       "/api/settings/security/challenge",
			Fields:       []string{"challenge_code"},
			SubmitLabels: []string{"Verify Challenge"},
		}},
		Controls: []RepoTraceControlHint{
			{Label: "Open Session History", TagName: "button", ControlType: "button", Region: "main", AriaControls: "session-history", AriaExpanded: "false"},
			{Label: "Delete Recovery Key", TagName: "button", ControlType: "button", FormAction: "/api/settings/security/recovery-key", FormMethod: "DELETE"},
		},
	}, 12, 8, 12)

	after := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:  "https://app.example.com/settings/security",
		CurrentPath: "/settings/security",
		Title:       "Security Settings",
		NavigationLinks: []RepoTraceNavigationLink{
			{Label: "Billing", Href: "/settings/billing", Region: "nav"},
			{Label: "Admin Console", Href: "/admin/users", Region: "nav"},
		},
		Forms: []RepoTraceFormHint{{
			Method:       "POST",
			Action:       "https://app.example.com/api/settings/security/challenge",
			Fields:       []string{"challenge_code"},
			SubmitLabels: []string{"Verify Challenge"},
		}},
		Controls: []RepoTraceControlHint{
			{Label: "Delete Recovery Key", TagName: "button", ControlType: "button", FormAction: "/api/settings/security/recovery-key", FormMethod: "DELETE"},
			{Label: "Open Session History", TagName: "button", ControlType: "button", Region: "main", AriaControls: "session-history", AriaExpanded: "false"},
		},
	}, 12, 8, 12)

	refresh := buildRepoTraceContextRefresh(before, after, 8)
	if refresh.Changed {
		t.Fatalf("expected pure reordering to produce no refresh delta, got %+v", refresh)
	}
	if len(refresh.NewNavigationLinks) != 0 || len(refresh.NewForms) != 0 || len(refresh.NewControls) != 0 || len(refresh.AppendedPlan) != 0 {
		t.Fatalf("expected no appended discovery surfaces for reordered context, got %+v", refresh)
	}
}

func TestNormalizeRepoTracePath(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{name: "full url", raw: "https://app.example.com/api/orders/42?tab=details", expected: "/api/orders/42"},
		{name: "template literal", raw: "/api/orders/${orderId}", expected: "/api/orders/:param"},
		{name: "relative api path", raw: "api/orders", expected: "/api/orders"},
		{name: "asset path rejected", raw: "/assets/app.js", expected: "/assets/app.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRepoTracePath(tt.raw); got != tt.expected {
				t.Fatalf("normalizeRepoTracePath(%q) = %q, want %q", tt.raw, got, tt.expected)
			}
		})
	}
}

func TestInferRepoTraceRouteFromFilePath(t *testing.T) {
	tests := []struct {
		name         string
		relPath      string
		expectedPath string
	}{
		{name: "next app route", relPath: "frontend/app/api/orders/[orderId]/route.ts", expectedPath: "/api/orders/:param"},
		{name: "next pages route", relPath: "frontend/pages/api/orders/index.ts", expectedPath: "/api/orders/index"},
		{name: "non route file", relPath: "frontend/src/orders.tsx", expectedPath: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := inferRepoTraceRouteFromFilePath(tt.relPath)
			if path != tt.expectedPath {
				t.Fatalf("inferRepoTraceRouteFromFilePath(%q) = %q, want %q", tt.relPath, path, tt.expectedPath)
			}
		})
	}
}
