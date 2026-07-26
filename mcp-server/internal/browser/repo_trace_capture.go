package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
)

const (
	defaultRepoTraceMaxFiles                 = 4000
	defaultRepoTraceMaxFileBytes       int64 = 1 << 20
	defaultRepoTraceMaxSeedHints             = 24
	defaultRepoTraceMaxNavigationHints       = 16
	defaultRepoTraceMaxControlHints          = 24
	defaultRepoTraceMaxPlanSteps             = 16
	defaultRepoTraceMaxFrontendMatches       = 12
	defaultRepoTraceMaxBackendMatches        = 12
	repoTraceSnippetLimit                    = 280
)

var (
	defaultRepoTraceIgnoreDirs = []string{
		".git",
		".browsernerd",
		"node_modules",
		".next",
		"dist",
		"build",
		"coverage",
		"vendor",
		"tmp",
		"temp",
		"bin",
		"obj",
		".turbo",
		".cache",
	}

	repoTraceSourceExtensions = map[string]bool{
		".cjs":    true,
		".cs":     true,
		".go":     true,
		".html":   true,
		".java":   true,
		".js":     true,
		".jsx":    true,
		".kt":     true,
		".mjs":    true,
		".php":    true,
		".py":     true,
		".rb":     true,
		".rs":     true,
		".svelte": true,
		".ts":     true,
		".tsx":    true,
		".vue":    true,
	}

	repoTraceAssetExtensions = map[string]bool{
		".css":   true,
		".gif":   true,
		".ico":   true,
		".jpeg":  true,
		".jpg":   true,
		".json":  true,
		".map":   true,
		".mp4":   true,
		".png":   true,
		".svg":   true,
		".webp":  true,
		".woff":  true,
		".woff2": true,
	}

	repoTraceStopTokens = map[string]bool{
		"about": true, "action": true, "admin": true, "api": true, "app": true,
		"auth": true, "backend": true, "button": true, "client": true, "component": true,
		"create": true, "data": true, "default": true, "dialog": true, "edit": true,
		"false": true, "form": true, "frontend": true, "get": true, "home": true,
		"index": true, "item": true, "list": true, "main": true, "new": true,
		"null": true, "page": true, "path": true, "post": true, "public": true,
		"request": true, "route": true, "routes": true, "screen": true, "server": true,
		"service": true, "state": true, "test": true, "true": true, "undefined": true,
		"update": true, "user": true, "view": true,
	}

	errRepoTraceFileLimit = errors.New("repo trace file limit reached")

	repoTraceAxiosMethodRe    = regexp.MustCompile("(?i)\\baxios\\.(get|post|put|patch|delete|options|head)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceFetchRe          = regexp.MustCompile("(?i)\\bfetch\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceClientMethodRe   = regexp.MustCompile("(?i)\\b(?:api|client|http|request)\\.(get|post|put|patch|delete|options|head)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceConfigURLRe      = regexp.MustCompile("(?i)\\burl\\s*:\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceSWRRe            = regexp.MustCompile("(?i)\\buseSWR\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceMethodOptionRe   = regexp.MustCompile("(?i)\\bmethod\\s*:\\s*[\"'`]([A-Z]+)[\"'`]")
	repoTraceExpressRouteRe   = regexp.MustCompile("(?i)\\b(?:router|app|server|group|r)\\.(get|post|put|patch|delete|options|head)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceDecoratorRouteRe = regexp.MustCompile("(?i)@(?:\\w+\\.)?(get|post|put|patch|delete|options|head)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceHandleFuncRe     = regexp.MustCompile("(?i)\\bHandleFunc\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceGoMethodRouteRe  = regexp.MustCompile("(?i)\\b(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceAuthRe           = regexp.MustCompile("(?i)\\b(requireAuth|withAuth|authMiddleware|authGuard|ensureAuth|jwt(?:Auth)?|passport|protected|authorize[A-Za-z0-9_]*|permission[A-Za-z0-9_]*|role[A-Za-z0-9_]*)\\b")
	repoTraceSchemaRe         = regexp.MustCompile("\\b([A-Za-z_][A-Za-z0-9_]*(?:Schema|Validator|Payload|Request|Body))\\b")
	repoTraceReqBodyFieldRe   = regexp.MustCompile("(?i)\\b(?:req|request)\\.body\\.([A-Za-z_][A-Za-z0-9_]*)\\b")
	repoTraceBodyFieldRe      = regexp.MustCompile("(?i)\\bbody\\.([A-Za-z_][A-Za-z0-9_]*)\\b")
	repoTraceFormValueRe      = regexp.MustCompile("(?i)\\bFormValue\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]")
	repoTraceTemplateExprRe   = regexp.MustCompile("\\$\\{[^}]+\\}")
	repoTraceBracketParamRe   = regexp.MustCompile("\\[[^\\]/]+\\]")
	repoTraceWhitespaceRe     = regexp.MustCompile("\\s+")
	repoTraceWordRe           = regexp.MustCompile("[A-Za-z0-9_\\-]+")
	repoTraceHexLikeRe        = regexp.MustCompile("(?i)^[a-f0-9]{8,}$")

	repoTraceWriteMethods = map[string]bool{
		"POST":   true,
		"PUT":    true,
		"PATCH":  true,
		"DELETE": true,
	}
	repoTraceWriteKeywords = []string{
		"save", "create", "submit", "send", "update", "invite", "upload", "import",
		"approve", "publish", "apply", "provision", "reset", "delete", "remove",
		"revoke", "archive", "disable", "connect", "sync",
	}
	repoTraceDestructiveKeywords = []string{
		"delete", "remove", "destroy", "purge", "reset", "revoke", "disconnect",
		"disable", "archive", "wipe", "erase", "forget", "clear all", "drop",
	}
	repoTraceAuthKeywords = []string{
		"auth", "login", "log in", "logout", "log out", "sign in", "sign out",
		"password", "passkey", "secret", "token", "otp", "mfa", "2fa",
		"two factor", "two-factor", "verify", "verification", "session",
		"security", "admin", "sso",
	}
	repoTraceSessionExitKeywords = []string{
		"logout", "log out", "sign out", "signoff", "sign-off",
	}
	repoTraceRevealKeywords = []string{
		"show", "show more", "expand", "collapse", "details", "reveal", "advanced",
		"filters", "filter", "options", "menu", "drawer", "panel", "modal",
		"dialog", "preview",
	}
)

type RepoTraceOptions struct {
	RootDir            string
	SearchRoots        []string
	IgnoreDirs         []string
	MaxFiles           int
	MaxFileBytes       int64
	MaxSeedHints       int
	MaxNavigationHints int
	MaxControlHints    int
	MaxPlanSteps       int
	MaxFrontendMatches int
	MaxBackendMatches  int
}

type RepoTraceFormHint struct {
	Method            string   `json:"method"`
	Action            string   `json:"action"`
	Fields            []string `json:"fields,omitempty"`
	SubmitLabels      []string `json:"submit_labels,omitempty"`
	WriteCapable      bool     `json:"write_capable,omitempty"`
	LikelyDestructive bool     `json:"likely_destructive,omitempty"`
	AuthSensitive     bool     `json:"auth_sensitive,omitempty"`
	HazardReasons     []string `json:"hazard_reasons,omitempty"`
}

type RepoTraceNavigationLink struct {
	Label             string   `json:"label,omitempty"`
	Href              string   `json:"href"`
	Path              string   `json:"path,omitempty"`
	Region            string   `json:"region,omitempty"`
	LinkType          string   `json:"link_type,omitempty"`
	Download          bool     `json:"download,omitempty"`
	AuthSensitive     bool     `json:"auth_sensitive,omitempty"`
	InternalCandidate bool     `json:"internal_candidate,omitempty"`
	DiscoveryReasons  []string `json:"discovery_reasons,omitempty"`
}

type RepoTraceControlHint struct {
	Label              string   `json:"label,omitempty"`
	TagName            string   `json:"tag_name,omitempty"`
	ControlType        string   `json:"control_type,omitempty"`
	Role               string   `json:"role,omitempty"`
	Region             string   `json:"region,omitempty"`
	Name               string   `json:"name,omitempty"`
	Href               string   `json:"href,omitempty"`
	Path               string   `json:"path,omitempty"`
	FormAction         string   `json:"form_action,omitempty"`
	FormMethod         string   `json:"form_method,omitempty"`
	Placeholder        string   `json:"placeholder,omitempty"`
	Autocomplete       string   `json:"autocomplete,omitempty"`
	AriaControls       string   `json:"aria_controls,omitempty"`
	AriaExpanded       string   `json:"aria_expanded,omitempty"`
	AriaHaspopup       string   `json:"aria_haspopup,omitempty"`
	DataHint           string   `json:"data_hint,omitempty"`
	Disabled           bool     `json:"disabled,omitempty"`
	WriteCapable       bool     `json:"write_capable,omitempty"`
	LikelyDestructive  bool     `json:"likely_destructive,omitempty"`
	AuthSensitive      bool     `json:"auth_sensitive,omitempty"`
	InternalNavigation bool     `json:"internal_navigation,omitempty"`
	RevealSurface      bool     `json:"reveal_surface,omitempty"`
	RevealKind         string   `json:"reveal_kind,omitempty"`
	HazardReasons      []string `json:"hazard_reasons,omitempty"`
	DiscoveryReasons   []string `json:"discovery_reasons,omitempty"`
}

type RepoTraceRequestSeed struct {
	Method    string `json:"method,omitempty"`
	URL       string `json:"url"`
	Path      string `json:"path"`
	Initiator string `json:"initiator,omitempty"`
}

type RepoTraceBrowserContext struct {
	CurrentURL      string                    `json:"current_url"`
	CurrentPath     string                    `json:"current_path"`
	Title           string                    `json:"title,omitempty"`
	Headings        []string                  `json:"headings,omitempty"`
	ComponentNames  []string                  `json:"component_names,omitempty"`
	Scripts         []string                  `json:"scripts,omitempty"`
	Forms           []RepoTraceFormHint       `json:"forms,omitempty"`
	NavigationLinks []RepoTraceNavigationLink `json:"navigation_links,omitempty"`
	Controls        []RepoTraceControlHint    `json:"controls,omitempty"`
	Requests        []RepoTraceRequestSeed    `json:"requests,omitempty"`
}

type RepoTraceSeed struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

type RepoTraceFrontendSite struct {
	ID             string   `json:"id"`
	EvidenceHandle string   `json:"evidence_handle"`
	FilePath       string   `json:"file_path"`
	Line           int      `json:"line"`
	Method         string   `json:"method,omitempty"`
	Endpoint       string   `json:"endpoint"`
	RequestCall    string   `json:"request_call,omitempty"`
	Confidence     float64  `json:"confidence"`
	Summary        string   `json:"summary"`
	MatchReasons   []string `json:"match_reasons,omitempty"`
}

type RepoTraceBackendExpectation struct {
	ID                 string   `json:"id"`
	EvidenceHandle     string   `json:"evidence_handle"`
	FilePath           string   `json:"file_path"`
	Line               int      `json:"line"`
	Method             string   `json:"method,omitempty"`
	RoutePath          string   `json:"route_path"`
	AuthExpectation    string   `json:"auth_expectation,omitempty"`
	PayloadExpectation string   `json:"payload_expectation,omitempty"`
	Confidence         float64  `json:"confidence"`
	Summary            string   `json:"summary"`
	MatchReasons       []string `json:"match_reasons,omitempty"`
}

type RepoTraceCorrelation struct {
	FrontendID string  `json:"frontend_id"`
	BackendID  string  `json:"backend_id"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

type RepoTraceEvidence struct {
	Handle     string  `json:"handle"`
	Kind       string  `json:"kind"`
	Summary    string  `json:"summary"`
	FilePath   string  `json:"file_path,omitempty"`
	Line       int     `json:"line,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type RepoTraceStats struct {
	FilesScanned                 int `json:"files_scanned"`
	FrontendMatches              int `json:"frontend_matches"`
	BackendMatches               int `json:"backend_matches"`
	CorrelationsFound            int `json:"correlations_found"`
	NavigationLinks              int `json:"navigation_links"`
	InternalNavigationLinks      int `json:"internal_navigation_links"`
	InternalNavigationControls   int `json:"internal_navigation_controls"`
	RevealControls               int `json:"reveal_controls"`
	WriteCapableForms            int `json:"write_capable_forms"`
	WriteCapableControls         int `json:"write_capable_controls"`
	DestructiveControls          int `json:"destructive_controls"`
	AuthSensitiveControls        int `json:"auth_sensitive_controls"`
	AuthSensitiveNavigationLinks int `json:"auth_sensitive_navigation_links"`
	AuditPlanSteps               int `json:"audit_plan_steps"`
}

type RepoTraceHazardSummary struct {
	WriteCapableForms            int `json:"write_capable_forms"`
	WriteCapableControls         int `json:"write_capable_controls"`
	DestructiveForms             int `json:"destructive_forms"`
	DestructiveControls          int `json:"destructive_controls"`
	AuthSensitiveForms           int `json:"auth_sensitive_forms"`
	AuthSensitiveControls        int `json:"auth_sensitive_controls"`
	NavigationLinks              int `json:"navigation_links"`
	InternalNavigationLinks      int `json:"internal_navigation_links"`
	InternalNavigationControls   int `json:"internal_navigation_controls"`
	RevealControls               int `json:"reveal_controls"`
	AuthSensitiveNavigationLinks int `json:"auth_sensitive_navigation_links"`
}

type RepoTracePlanStep struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Target      string   `json:"target"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	Mode        string   `json:"mode"`
	HazardTypes []string `json:"hazard_types,omitempty"`
	Source      string   `json:"source"`
	Summary     string   `json:"summary"`
}

type RepoTraceReport struct {
	TraceID         string                        `json:"trace_id"`
	SessionID       string                        `json:"session_id"`
	GeneratedAt     time.Time                     `json:"generated_at"`
	RootDir         string                        `json:"root_dir"`
	SearchRoots     []string                      `json:"search_roots"`
	BrowserContext  RepoTraceBrowserContext       `json:"browser_context"`
	HazardSummary   RepoTraceHazardSummary        `json:"hazard_summary"`
	AuditPlan       []RepoTracePlanStep           `json:"audit_plan,omitempty"`
	Seeds           []RepoTraceSeed               `json:"seeds"`
	FrontendSites   []RepoTraceFrontendSite       `json:"frontend_sites"`
	BackendMatches  []RepoTraceBackendExpectation `json:"backend_matches"`
	Correlations    []RepoTraceCorrelation        `json:"correlations"`
	Evidence        []RepoTraceEvidence           `json:"evidence"`
	EvidenceHandles []string                      `json:"evidence_handles"`
	Facts           []mangle.Fact                 `json:"facts,omitempty"`
	Warnings        []string                      `json:"warnings,omitempty"`
	Stats           RepoTraceStats                `json:"stats"`
}

type repoTraceFactReader interface {
	FactsByPredicate(predicate string) []mangle.Fact
}

type repoTraceSeedSet struct {
	pagePaths       []string
	apiPaths        []string
	pageTokens      map[string]bool
	componentTokens map[string]bool
	fieldTokens     map[string]bool
	seeds           []RepoTraceSeed
}

type repoTraceScanner struct {
	rootDir       string
	searchRoots   []string
	cfg           config.RepoTraceConfig
	seeds         repoTraceSeedSet
	frontendSites []RepoTraceFrontendSite
	backendRoutes []RepoTraceBackendExpectation
	filesScanned  int
	warnings      []string
}

func (m *SessionManager) TraceRepositoryContext(ctx context.Context, sessionID string, opts *RepoTraceOptions) (*RepoTraceReport, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session_id is required")
	}

	page, ok := m.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}

	traceCfg := resolveRepoTraceConfig(m.cfg.RepoTrace, opts)
	if !traceCfg.Enabled {
		return nil, errors.New("repo trace is disabled")
	}

	browserCtx, warnings, err := m.collectRepoTraceBrowserContext(ctx, sessionID, page, traceCfg)
	if err != nil {
		return nil, err
	}

	report, err := traceRepositoryWorkspace(ctx, sessionID, browserCtx, traceCfg)
	if err != nil {
		return nil, err
	}
	report.Warnings = append(report.Warnings, warnings...)

	if m.engine != nil && len(report.Facts) > 0 {
		if err := m.engine.AddFacts(ctx, report.Facts); err != nil {
			return nil, fmt.Errorf("emit repo trace facts: %w", err)
		}
	}

	return report, nil
}

func (m *SessionManager) collectRepoTraceBrowserContext(ctx context.Context, sessionID string, page *rod.Page, traceCfg config.RepoTraceConfig) (RepoTraceBrowserContext, []string, error) {
	seedLimit := traceCfg.MaxSeedHints
	if seedLimit <= 0 {
		seedLimit = defaultRepoTraceMaxSeedHints
	}
	navLimit := traceCfg.MaxNavigationHints
	if navLimit <= 0 {
		navLimit = defaultRepoTraceMaxNavigationHints
	}
	controlLimit := traceCfg.MaxControlHints
	if controlLimit <= 0 {
		controlLimit = defaultRepoTraceMaxControlHints
	}

	res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
		JS: fmt.Sprintf(`
		() => {
			const seedLimit = %d;
			const navLimit = %d;
			const controlLimit = %d;
			const clean = (value) => String(value || '').replace(/\s+/g, ' ').trim();
			const unique = (values, limit = seedLimit) => Array.from(new Set((values || []).map(clean).filter(Boolean))).slice(0, limit);
			const labelFor = (el) => unique([
				el && el.getAttribute && el.getAttribute('aria-label'),
				el && el.getAttribute && el.getAttribute('title'),
				el && el.getAttribute && el.getAttribute('placeholder'),
				el && el.value,
				el && el.innerText,
				el && el.textContent,
				...(Array.from((el && el.labels) || []).map((label) => label && (label.innerText || label.textContent || ''))),
			], 1)[0] || '';
			const regionFor = (el) => {
				if (!el || !el.closest) return 'content';
				if (el.closest('[aria-label*="breadcrumb" i], .breadcrumb, [data-breadcrumb]')) return 'breadcrumb';
				if (el.closest('[aria-label*="pagination" i], .pagination, [data-pagination]')) return 'pagination';
				if (el.closest('nav, [role="navigation"]')) return 'nav';
				if (el.closest('aside, [role="complementary"], [data-sidebar], .sidebar')) return 'sidebar';
				if (el.closest('footer')) return 'footer';
				if (el.closest('main, [role="main"]')) return 'main';
				return 'content';
			};
			const collectComponents = () => {
				try {
					const root = document.querySelector('[data-reactroot]') || document.getElementById('root') || document.body;
					if (!root) return [];
					const fiberKey = Object.keys(root).find((key) => key.startsWith('__reactFiber'));
					if (!fiberKey) return [];
					const seen = new Set();
					const stack = [root[fiberKey]];
					const names = [];
					while (stack.length && names.length < seedLimit) {
						const fiber = stack.pop();
						if (!fiber || seen.has(fiber)) continue;
						seen.add(fiber);
						const name = (fiber.type && (fiber.type.displayName || fiber.type.name)) ||
							(fiber.elementType && fiber.elementType.name) ||
							'';
						if (name) names.push(name);
						if (fiber.child) stack.push(fiber.child);
						if (fiber.sibling) stack.push(fiber.sibling);
					}
					return unique(names);
				} catch (e) {
					return [];
				}
			};
			const links = Array.from(document.querySelectorAll('a[href]'))
				.map((el) => ({
					label: labelFor(el),
					href: clean(el.getAttribute('href') || el.href || ''),
					region: regionFor(el),
					download: !!el.getAttribute('download'),
				}))
				.filter((link) => link.href)
				.slice(0, navLimit);
			const controls = Array.from(document.querySelectorAll('button, summary, input:not([type="hidden"]), textarea, select, [role="button"], [role="tab"], [role="menuitem"], [aria-controls], [aria-expanded], [aria-haspopup], [contenteditable="true"], a[role="button"]'))
				.map((el) => {
					const form = el.form || (el.closest && el.closest('form'));
					return {
						label: labelFor(el),
						tagName: clean((el.tagName || '').toLowerCase()),
						controlType: clean(((el.getAttribute && el.getAttribute('type')) || el.type || '').toLowerCase()),
						role: clean((el.getAttribute && el.getAttribute('role')) || ''),
						region: regionFor(el),
						name: clean((el.getAttribute && el.getAttribute('name')) || el.id || ''),
						href: clean((el.getAttribute && el.getAttribute('href')) || ''),
						formAction: clean(((el.getAttribute && el.getAttribute('formaction')) || (form && (form.getAttribute('action') || form.action)) || '')),
						formMethod: clean((((el.getAttribute && el.getAttribute('formmethod')) || (form && form.method) || 'GET').toUpperCase())),
						placeholder: clean((el.getAttribute && el.getAttribute('placeholder')) || ''),
						autocomplete: clean((el.getAttribute && el.getAttribute('autocomplete')) || ''),
						ariaControls: clean((el.getAttribute && el.getAttribute('aria-controls')) || ''),
						ariaExpanded: clean((el.getAttribute && el.getAttribute('aria-expanded')) || ''),
						ariaHaspopup: clean((el.getAttribute && el.getAttribute('aria-haspopup')) || ''),
						dataHint: clean(
							(el.getAttribute && (
								el.getAttribute('data-action') ||
								el.getAttribute('data-testid') ||
								el.getAttribute('data-qa') ||
								el.getAttribute('data-cy') ||
								el.getAttribute('data-test')
							)) || ''
						),
						disabled: !!el.disabled,
					};
				})
				.filter((control) => control.label || control.name || control.href || control.formAction)
				.slice(0, controlLimit);

			return {
				href: location.href,
				path: location.pathname || '/',
				title: document.title || '',
				headings: unique(Array.from(document.querySelectorAll('h1, h2, [data-testid], [aria-label]'))
					.map((el) => (el.textContent || el.getAttribute('data-testid') || el.getAttribute('aria-label') || '').trim())
					.filter(Boolean))),
				scripts: unique(Array.from(document.querySelectorAll('script[src]'))
					.map((el) => el.getAttribute('src') || '')
					.filter(Boolean)),
				forms: Array.from(document.forms || []).slice(0, seedLimit).map((form) => ({
					method: (form.method || 'GET').toUpperCase(),
					action: form.getAttribute('action') || form.action || location.href,
					fields: unique(Array.from(form.elements || [])
						.map((el) => el.name || el.id || (el.getAttribute && el.getAttribute('data-testid')) || '')
						.filter(Boolean)),
					submitLabels: unique(Array.from(form.querySelectorAll('button, input[type="submit"], input[type="button"], input[type="reset"]'))
						.map((el) => labelFor(el))
						.filter(Boolean)),
				})),
				navigationLinks: links,
				controls: controls,
				componentNames: collectComponents(),
			};
		}
		`, seedLimit, navLimit, controlLimit),
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil {
		return RepoTraceBrowserContext{}, nil, fmt.Errorf("collect repo trace browser context: %w", err)
	}

	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return RepoTraceBrowserContext{}, nil, fmt.Errorf("marshal repo trace browser context: %w", err)
	}

	var browserCtx struct {
		Href            string                    `json:"href"`
		Path            string                    `json:"path"`
		Title           string                    `json:"title"`
		Headings        []string                  `json:"headings"`
		Scripts         []string                  `json:"scripts"`
		Forms           []RepoTraceFormHint       `json:"forms"`
		NavigationLinks []RepoTraceNavigationLink `json:"navigationLinks"`
		Controls        []RepoTraceControlHint    `json:"controls"`
		ComponentNames  []string                  `json:"componentNames"`
	}
	if err := json.Unmarshal(raw, &browserCtx); err != nil {
		return RepoTraceBrowserContext{}, nil, fmt.Errorf("decode repo trace browser context: %w", err)
	}

	ctxOut := normalizeRepoTraceBrowserContext(RepoTraceBrowserContext{
		CurrentURL:      strings.TrimSpace(browserCtx.Href),
		CurrentPath:     normalizeRepoTracePath(browserCtx.Path),
		Title:           strings.TrimSpace(browserCtx.Title),
		Headings:        uniqueOrderedStrings(browserCtx.Headings),
		ComponentNames:  uniqueOrderedStrings(browserCtx.ComponentNames),
		Scripts:         uniqueOrderedStrings(browserCtx.Scripts),
		Forms:           browserCtx.Forms,
		NavigationLinks: browserCtx.NavigationLinks,
		Controls:        browserCtx.Controls,
		Requests:        m.collectRepoTraceRequests(sessionID, seedLimit),
	}, seedLimit, navLimit, controlLimit)

	warnings := make([]string, 0, 1)
	if ctxOut.CurrentPath == "" {
		ctxOut.CurrentPath = "/"
		warnings = append(warnings, "current browser path was empty; falling back to root path")
	}

	return ctxOut, warnings, nil
}

func (m *SessionManager) collectRepoTraceRequests(sessionID string, limit int) []RepoTraceRequestSeed {
	if limit <= 0 {
		limit = defaultRepoTraceMaxSeedHints
	}
	reader, ok := m.engine.(repoTraceFactReader)
	if !ok {
		return nil
	}

	facts := reader.FactsByPredicate("net_request")
	if len(facts) == 0 {
		return nil
	}

	requests := make([]RepoTraceRequestSeed, 0, limit)
	seen := make(map[string]bool)
	for i := len(facts) - 1; i >= 0 && len(requests) < limit; i-- {
		fact := facts[i]
		if len(fact.Args) < 6 {
			continue
		}
		if fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", fact.Args[2])))
		rawURL := strings.TrimSpace(fmt.Sprintf("%v", fact.Args[3]))
		path := normalizeRepoTracePath(rawURL)
		if path == "" || !looksLikeRepoTracePath(path) {
			continue
		}
		key := method + " " + path
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, RepoTraceRequestSeed{
			Method:    method,
			URL:       rawURL,
			Path:      path,
			Initiator: strings.TrimSpace(fmt.Sprintf("%v", fact.Args[4])),
		})
	}

	return requests
}

func normalizeRepoTraceBrowserContext(ctx RepoTraceBrowserContext, seedLimit, navLimit, controlLimit int) RepoTraceBrowserContext {
	if seedLimit <= 0 {
		seedLimit = defaultRepoTraceMaxSeedHints
	}
	if navLimit <= 0 {
		navLimit = defaultRepoTraceMaxNavigationHints
	}
	if controlLimit <= 0 {
		controlLimit = defaultRepoTraceMaxControlHints
	}

	ctx.CurrentPath = firstNonEmpty(normalizeRepoTracePath(ctx.CurrentPath), "/")
	ctx.Forms = normalizeRepoTraceFormHints(ctx.Forms, ctx.CurrentURL, seedLimit)
	ctx.NavigationLinks = normalizeRepoTraceNavigationLinks(ctx.NavigationLinks, ctx.CurrentURL, ctx.CurrentPath, navLimit)
	ctx.Controls = normalizeRepoTraceControlHints(ctx.Controls, ctx.CurrentURL, ctx.CurrentPath, controlLimit)
	ctx.Requests = normalizeRepoTraceRequestSeeds(ctx.Requests, seedLimit)
	return ctx
}

func normalizeRepoTraceFormHints(forms []RepoTraceFormHint, currentURL string, limit int) []RepoTraceFormHint {
	if limit <= 0 {
		limit = defaultRepoTraceMaxSeedHints
	}
	out := make([]RepoTraceFormHint, 0, len(forms))
	seen := make(map[string]bool)
	for _, form := range forms {
		form.Method = normalizeRepoTraceMethod(form.Method)
		form.Action = strings.TrimSpace(form.Action)
		form.Fields = uniqueOrderedStrings(form.Fields)
		form.SubmitLabels = uniqueOrderedStrings(form.SubmitLabels)
		form.WriteCapable, form.LikelyDestructive, form.AuthSensitive, form.HazardReasons = classifyRepoTraceFormHint(form, currentURL)
		key := strings.Join([]string{
			form.Method,
			repoTraceOwnedPath(form.Action, currentURL),
			strings.Join(form.Fields, ","),
			strings.Join(form.SubmitLabels, ","),
		}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, form)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeRepoTraceNavigationLinks(links []RepoTraceNavigationLink, currentURL, currentPath string, limit int) []RepoTraceNavigationLink {
	if limit <= 0 {
		limit = defaultRepoTraceMaxNavigationHints
	}
	out := make([]RepoTraceNavigationLink, 0, len(links))
	seen := make(map[string]bool)
	for _, link := range links {
		link.Label = compactRepoTraceSnippet(link.Label)
		link.Href = strings.TrimSpace(link.Href)
		link.Region = normalizeRepoTraceRegion(link.Region)
		link.LinkType = determineRepoTraceLinkType(link.Href, currentURL, link.Download)
		if link.LinkType == "internal" || link.LinkType == "download" {
			link.Path = repoTraceOwnedPath(link.Href, currentURL)
		} else {
			link.Path = ""
		}
		link.AuthSensitive = repoTraceKeywordMatch(strings.Join([]string{link.Label, link.Path, link.Href}, " "), repoTraceAuthKeywords) != ""
		link.InternalCandidate, link.DiscoveryReasons = classifyRepoTraceNavigationLink(link, currentPath)
		key := strings.Join([]string{link.LinkType, link.Region, link.Path, link.Label, link.Href}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, link)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeRepoTraceControlHints(controls []RepoTraceControlHint, currentURL, currentPath string, limit int) []RepoTraceControlHint {
	if limit <= 0 {
		limit = defaultRepoTraceMaxControlHints
	}
	out := make([]RepoTraceControlHint, 0, len(controls))
	seen := make(map[string]bool)
	for _, control := range controls {
		control.Label = compactRepoTraceSnippet(control.Label)
		control.TagName = strings.ToLower(strings.TrimSpace(control.TagName))
		control.ControlType = strings.ToLower(strings.TrimSpace(control.ControlType))
		control.Role = strings.ToLower(strings.TrimSpace(control.Role))
		control.Region = normalizeRepoTraceRegion(control.Region)
		control.Name = strings.TrimSpace(control.Name)
		control.Href = strings.TrimSpace(control.Href)
		control.FormAction = strings.TrimSpace(control.FormAction)
		control.FormMethod = normalizeRepoTraceMethod(control.FormMethod)
		control.Placeholder = compactRepoTraceSnippet(control.Placeholder)
		control.Autocomplete = strings.TrimSpace(control.Autocomplete)
		control.AriaControls = strings.TrimSpace(control.AriaControls)
		control.AriaExpanded = strings.ToLower(strings.TrimSpace(control.AriaExpanded))
		control.AriaHaspopup = strings.ToLower(strings.TrimSpace(control.AriaHaspopup))
		control.DataHint = compactRepoTraceSnippet(control.DataHint)
		control.Path = repoTraceOwnedPath(firstNonEmpty(control.FormAction, control.Href), currentURL)
		control.WriteCapable, control.LikelyDestructive, control.AuthSensitive, control.HazardReasons = classifyRepoTraceControlHint(control)
		control.InternalNavigation, control.RevealSurface, control.RevealKind, control.DiscoveryReasons = classifyRepoTraceControlDiscovery(control, currentPath)
		key := strings.Join([]string{
			control.TagName,
			control.ControlType,
			control.Role,
			control.Name,
			control.Path,
			control.Label,
		}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, control)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeRepoTraceRequestSeeds(requests []RepoTraceRequestSeed, limit int) []RepoTraceRequestSeed {
	if limit <= 0 {
		limit = defaultRepoTraceMaxSeedHints
	}
	out := make([]RepoTraceRequestSeed, 0, len(requests))
	seen := make(map[string]bool)
	for _, request := range requests {
		request.Method = normalizeRepoTraceMethod(request.Method)
		request.URL = strings.TrimSpace(request.URL)
		request.Path = normalizeRepoTracePath(firstNonEmpty(request.Path, request.URL))
		request.Initiator = strings.TrimSpace(request.Initiator)
		key := request.Method + "|" + request.Path + "|" + request.Initiator
		if request.Path == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, request)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func classifyRepoTraceFormHint(form RepoTraceFormHint, currentURL string) (bool, bool, bool, []string) {
	method := normalizeRepoTraceMethod(form.Method)
	path := repoTraceOwnedPath(form.Action, currentURL)
	text := strings.Join([]string{
		method,
		form.Action,
		path,
		strings.Join(form.Fields, " "),
		strings.Join(form.SubmitLabels, " "),
	}, " ")
	reasons := make([]string, 0, 4)
	writeCapable := repoTraceWriteMethods[method]
	if writeCapable {
		reasons = append(reasons, "mutating method "+method)
	}
	if keyword := repoTraceKeywordMatch(text, repoTraceWriteKeywords); keyword != "" {
		writeCapable = true
		reasons = append(reasons, "write keyword "+keyword)
	}
	destructive := method == "DELETE"
	if destructive {
		reasons = append(reasons, "delete method")
	}
	if keyword := repoTraceKeywordMatch(text, repoTraceDestructiveKeywords); keyword != "" {
		destructive = true
		writeCapable = true
		reasons = append(reasons, "destructive keyword "+keyword)
	}
	authSensitive := repoTraceKeywordMatch(text, repoTraceAuthKeywords) != ""
	if authSensitive {
		reasons = append(reasons, "auth-sensitive fields or action")
	}
	return writeCapable, destructive, authSensitive, uniqueOrderedStrings(reasons)
}

func classifyRepoTraceNavigationLink(link RepoTraceNavigationLink, currentPath string) (bool, []string) {
	path := normalizeRepoTracePath(link.Path)
	if link.LinkType != "internal" || !repoTraceIsInternalPagePath(path, currentPath) {
		return false, nil
	}

	text := strings.Join([]string{link.Label, path, link.Href}, " ")
	if repoTraceKeywordMatch(text, repoTraceSessionExitKeywords) != "" {
		return false, nil
	}

	reasons := []string{"internal route candidate"}
	if link.Region != "" && link.Region != "content" {
		reasons = append(reasons, link.Region+" landmark")
	}
	if link.AuthSensitive {
		reasons = append(reasons, "auth-sensitive destination")
	}
	return true, uniqueOrderedStrings(reasons)
}

func classifyRepoTraceControlHint(control RepoTraceControlHint) (bool, bool, bool, []string) {
	controlType := strings.ToLower(strings.TrimSpace(control.ControlType))
	tagName := strings.ToLower(strings.TrimSpace(control.TagName))
	role := strings.ToLower(strings.TrimSpace(control.Role))
	formMethod := normalizeRepoTraceMethod(control.FormMethod)
	text := strings.Join([]string{
		control.Label,
		control.Name,
		control.Path,
		control.FormAction,
		control.Placeholder,
		control.Autocomplete,
		control.AriaControls,
		control.AriaExpanded,
		control.AriaHaspopup,
		control.DataHint,
		controlType,
		tagName,
		role,
	}, " ")
	reasons := make([]string, 0, 5)
	writeCapable := repoTraceWriteMethods[formMethod] ||
		tagName == "textarea" ||
		tagName == "select" ||
		controlType == "text" ||
		controlType == "email" ||
		controlType == "password" ||
		controlType == "search" ||
		controlType == "url" ||
		controlType == "tel" ||
		controlType == "number" ||
		controlType == "date" ||
		controlType == "datetime-local" ||
		controlType == "file"
	if tagName == "input" && controlType == "" {
		writeCapable = true
	}
	if (controlType == "submit" || controlType == "image") && (repoTraceWriteMethods[formMethod] || repoTraceKeywordMatch(text, repoTraceWriteKeywords) != "") {
		writeCapable = true
	}
	if writeCapable {
		reasons = append(reasons, "interactive write surface")
	}
	if repoTraceWriteMethods[formMethod] {
		reasons = append(reasons, "linked to "+formMethod+" form")
	}
	if tagName == "button" || controlType == "button" || role == "button" {
		if repoTraceWriteMethods[formMethod] {
			writeCapable = true
		}
	}
	if keyword := repoTraceKeywordMatch(text, repoTraceWriteKeywords); keyword != "" {
		writeCapable = true
		reasons = append(reasons, "write keyword "+keyword)
	}
	destructive := controlType == "reset" || formMethod == "DELETE"
	if destructive {
		reasons = append(reasons, "destructive control type or method")
	}
	if keyword := repoTraceKeywordMatch(text, repoTraceDestructiveKeywords); keyword != "" {
		destructive = true
		writeCapable = true
		reasons = append(reasons, "destructive keyword "+keyword)
	}
	authSensitive := controlType == "password" ||
		strings.Contains(strings.ToLower(control.Autocomplete), "password") ||
		strings.Contains(strings.ToLower(control.Autocomplete), "one-time-code")
	if authSensitive {
		reasons = append(reasons, "credential control metadata")
	}
	if keyword := repoTraceKeywordMatch(text, repoTraceAuthKeywords); keyword != "" {
		authSensitive = true
		reasons = append(reasons, "auth-sensitive keyword "+keyword)
	}
	return writeCapable, destructive, authSensitive, uniqueOrderedStrings(reasons)
}

func classifyRepoTraceControlDiscovery(control RepoTraceControlHint, currentPath string) (bool, bool, string, []string) {
	text := strings.Join([]string{
		control.Label,
		control.Name,
		control.Path,
		control.FormAction,
		control.Href,
		control.Placeholder,
		control.DataHint,
		control.AriaControls,
		control.AriaExpanded,
		control.AriaHaspopup,
		control.ControlType,
		control.TagName,
		control.Role,
	}, " ")

	revealKind, revealReasons := classifyRepoTraceRevealSurface(control, text)
	if revealKind != "" {
		return false, true, revealKind, revealReasons
	}

	if !repoTraceIsNavigationActivator(control) {
		return false, false, "", nil
	}
	if !repoTraceIsInternalPagePath(control.Path, currentPath) {
		return false, false, "", nil
	}
	if control.LikelyDestructive || repoTraceWriteMethods[normalizeRepoTraceMethod(control.FormMethod)] {
		return false, false, "", nil
	}
	if repoTraceKeywordMatch(text, repoTraceSessionExitKeywords) != "" {
		return false, false, "", nil
	}

	reasons := []string{"internal navigation control"}
	if control.Region != "" && control.Region != "content" {
		reasons = append(reasons, control.Region+" landmark")
	}
	if control.AuthSensitive {
		reasons = append(reasons, "auth-sensitive destination")
	}
	return true, false, "", uniqueOrderedStrings(reasons)
}

func classifyRepoTraceRevealSurface(control RepoTraceControlHint, text string) (string, []string) {
	reasons := make([]string, 0, 4)
	switch {
	case control.Role == "tab":
		reasons = append(reasons, "tab control")
		if control.AriaControls != "" {
			reasons = append(reasons, "controls "+control.AriaControls)
		}
		return "tab", uniqueOrderedStrings(reasons)
	case control.TagName == "summary":
		return "disclosure", []string{"summary disclosure"}
	case control.AriaExpanded != "":
		reasons = append(reasons, "aria-expanded surface")
		if control.AriaControls != "" {
			reasons = append(reasons, "controls "+control.AriaControls)
		}
		return "disclosure", uniqueOrderedStrings(reasons)
	case control.AriaControls != "":
		reasons = append(reasons, "controls "+control.AriaControls)
		return "controlled_region", uniqueOrderedStrings(reasons)
	case control.AriaHaspopup != "" && control.AriaHaspopup != "false":
		kind := control.AriaHaspopup
		switch kind {
		case "true", "menu":
			kind = "menu"
		case "dialog":
			kind = "dialog"
		case "listbox":
			kind = "listbox"
		default:
			kind = "popup"
		}
		return kind, []string{"opens " + kind}
	}

	if keyword := repoTraceKeywordMatch(strings.ToLower(text), repoTraceRevealKeywords); keyword != "" {
		kind := "reveal"
		switch keyword {
		case "filters", "filter":
			kind = "filter_panel"
		case "menu":
			kind = "menu"
		case "drawer":
			kind = "drawer"
		case "modal", "dialog":
			kind = "dialog"
		case "details", "expand", "collapse":
			kind = "disclosure"
		case "preview":
			kind = "preview"
		}
		return kind, []string{"reveal keyword " + keyword}
	}

	return "", nil
}

func repoTraceIsNavigationActivator(control RepoTraceControlHint) bool {
	switch control.TagName {
	case "button", "summary", "a":
		return true
	}
	switch control.ControlType {
	case "button", "submit", "image":
		return true
	}
	return control.Role == "button" || control.Role == "tab" || control.Role == "menuitem"
}

func determineRepoTraceLinkType(href, currentURL string, download bool) string {
	href = strings.TrimSpace(href)
	lower := strings.ToLower(href)
	switch {
	case download:
		return "download"
	case strings.HasPrefix(lower, "#"):
		return "hash"
	case strings.HasPrefix(lower, "javascript:"):
		return "javascript"
	case strings.HasPrefix(lower, "mailto:"):
		return "mailto"
	case strings.HasPrefix(lower, "tel:"):
		return "tel"
	}
	parsed, err := url.Parse(href)
	if err == nil && parsed.Host != "" {
		current, currentErr := url.Parse(strings.TrimSpace(currentURL))
		if currentErr == nil && current.Host != "" && !strings.EqualFold(parsed.Host, current.Host) {
			return "external"
		}
	}
	if repoTraceOwnedPath(href, currentURL) != "" {
		return "internal"
	}
	return "unknown"
}

func repoTraceOwnedPath(raw, currentURL string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "\"'`"))
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "#"),
		strings.HasPrefix(lower, "javascript:"),
		strings.HasPrefix(lower, "mailto:"),
		strings.HasPrefix(lower, "tel:"):
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		current, currentErr := url.Parse(strings.TrimSpace(currentURL))
		if currentErr == nil && current.Host != "" && !strings.EqualFold(parsed.Host, current.Host) {
			return ""
		}
	}
	return normalizeRepoTracePath(raw)
}

func repoTraceIsInternalPagePath(path, currentPath string) bool {
	path = normalizeRepoTracePath(path)
	currentPath = normalizeRepoTracePath(currentPath)
	if path == "" {
		return false
	}
	if path == currentPath {
		return false
	}
	if strings.HasPrefix(path, "/api/") {
		return false
	}
	if path != "/" && repoTraceAssetExtensions[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	return strings.HasPrefix(path, "/")
}

func normalizeRepoTraceRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "breadcrumb":
		return "breadcrumb"
	case "pagination":
		return "pagination"
	case "nav":
		return "nav"
	case "sidebar":
		return "sidebar"
	case "footer":
		return "footer"
	case "main":
		return "main"
	default:
		return "content"
	}
}

func normalizeRepoTraceMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "GET"
	}
	return method
}
