package browser

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RepoTraceContextRefresh captures a bounded post-action browser-context refresh
// so recursive audit execution can decide whether newly exposed surfaces should
// extend the audit plan.
type RepoTraceContextRefresh struct {
	CurrentContext     RepoTraceBrowserContext   `json:"current_context"`
	Changed            bool                      `json:"changed"`
	RouteChanged       bool                      `json:"route_changed,omitempty"`
	SnapshotChanged    bool                      `json:"snapshot_changed,omitempty"`
	PlanExpanded       bool                      `json:"plan_expanded,omitempty"`
	NewNavigationLinks []RepoTraceNavigationLink `json:"new_navigation_links,omitempty"`
	NewForms           []RepoTraceFormHint       `json:"new_forms,omitempty"`
	NewControls        []RepoTraceControlHint    `json:"new_controls,omitempty"`
	AppendedPlan       []RepoTracePlanStep       `json:"appended_plan,omitempty"`
	Warnings           []string                  `json:"warnings,omitempty"`
}

type repoTracePlanBuilder struct {
	maxSteps int
	steps    []RepoTracePlanStep
	seen     map[string]bool
}

func newRepoTracePlanBuilder(maxSteps int) *repoTracePlanBuilder {
	if maxSteps <= 0 {
		maxSteps = defaultRepoTraceMaxPlanSteps
	}
	return &repoTracePlanBuilder{
		maxSteps: maxSteps,
		steps:    make([]RepoTracePlanStep, 0, maxSteps),
		seen:     make(map[string]bool),
	}
}

func (b *repoTracePlanBuilder) add(step RepoTracePlanStep) {
	if b == nil || len(b.steps) >= b.maxSteps {
		return
	}
	key := repoTracePlanStepKey(step)
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	step.ID = fmt.Sprintf("step-%d", len(b.steps)+1)
	b.steps = append(b.steps, step)
}

func repoTracePlanStepKey(step RepoTracePlanStep) string {
	return strings.Join([]string{
		step.Kind,
		step.Target,
		step.Method,
		step.Path,
		step.Mode,
		step.Source,
	}, "|")
}

func appendRepoTraceCaptureContextStep(builder *repoTracePlanBuilder, browserCtx RepoTraceBrowserContext) {
	builder.add(RepoTracePlanStep{
		Kind:    "capture_context",
		Target:  firstNonEmpty(browserCtx.CurrentPath, "/"),
		Path:    firstNonEmpty(browserCtx.CurrentPath, "/"),
		Mode:    "read_only",
		Source:  "browser.context",
		Summary: "Capture the current route, headings, and component hints before inspecting higher-risk surfaces.",
	})
}

func appendRepoTraceRevealPlanSteps(builder *repoTracePlanBuilder, controls []RepoTraceControlHint) {
	candidates := append([]RepoTraceControlHint(nil), controls...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].AuthSensitive != candidates[j].AuthSensitive {
			return candidates[i].AuthSensitive
		}
		if candidates[i].Region != candidates[j].Region {
			return candidates[i].Region < candidates[j].Region
		}
		if candidates[i].RevealKind != candidates[j].RevealKind {
			return candidates[i].RevealKind < candidates[j].RevealKind
		}
		return candidates[i].Label < candidates[j].Label
	})
	for _, control := range candidates {
		if !control.RevealSurface {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		target := firstNonEmpty(control.Label, control.Name, control.AriaControls, control.Path)
		builder.add(RepoTracePlanStep{
			Kind:        "reveal_surface",
			Target:      target,
			Method:      normalizeRepoTraceMethod(control.FormMethod),
			Path:        control.Path,
			Mode:        repoTracePlanMode(hazards, true),
			HazardTypes: hazards,
			Source:      "browser.control",
			Summary:     fmt.Sprintf("Use the %s %s control to reveal more page context before leaving the current route.", firstNonEmpty(control.Region, "content"), firstNonEmpty(control.RevealKind, "reveal")),
		})
	}
}

func appendRepoTraceNavigationPlanSteps(builder *repoTracePlanBuilder, navLinks []RepoTraceNavigationLink) {
	links := append([]RepoTraceNavigationLink(nil), navLinks...)
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].InternalCandidate != links[j].InternalCandidate {
			return links[i].InternalCandidate
		}
		if links[i].AuthSensitive != links[j].AuthSensitive {
			return links[i].AuthSensitive
		}
		if links[i].Region != links[j].Region {
			return links[i].Region < links[j].Region
		}
		if links[i].Path != links[j].Path {
			return links[i].Path < links[j].Path
		}
		return links[i].Label < links[j].Label
	})
	for _, link := range links {
		if !link.InternalCandidate {
			continue
		}
		hazards := repoTraceHazardTypes(false, false, link.AuthSensitive)
		target := firstNonEmpty(link.Label, link.Path, link.Href)
		builder.add(RepoTracePlanStep{
			Kind:        "map_navigation",
			Target:      target,
			Path:        link.Path,
			Mode:        repoTracePlanMode(hazards, false),
			HazardTypes: hazards,
			Source:      "browser.navigation_link",
			Summary:     fmt.Sprintf("Review the %s internal route candidate before deeper repo recursion.", firstNonEmpty(link.Region, "content")),
		})
	}
}

func appendRepoTraceNavigationControlPlanSteps(builder *repoTracePlanBuilder, controls []RepoTraceControlHint) {
	candidates := append([]RepoTraceControlHint(nil), controls...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].AuthSensitive != candidates[j].AuthSensitive {
			return candidates[i].AuthSensitive
		}
		if candidates[i].Region != candidates[j].Region {
			return candidates[i].Region < candidates[j].Region
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Label < candidates[j].Label
	})
	for _, control := range candidates {
		if !control.InternalNavigation {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		target := firstNonEmpty(control.Label, control.Name, control.Path, control.Href)
		builder.add(RepoTracePlanStep{
			Kind:        "map_navigation",
			Target:      target,
			Method:      normalizeRepoTraceMethod(control.FormMethod),
			Path:        control.Path,
			Mode:        repoTracePlanMode(hazards, false),
			HazardTypes: hazards,
			Source:      "browser.control",
			Summary:     fmt.Sprintf("Review the %s control as an internal navigation candidate before activation.", firstNonEmpty(control.Region, "content")),
		})
	}
}

func appendRepoTraceFormPlanSteps(builder *repoTracePlanBuilder, browserCtx RepoTraceBrowserContext, forms []RepoTraceFormHint) {
	candidates := append([]RepoTraceFormHint(nil), forms...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if repoTraceHazardRank(candidates[i].WriteCapable, candidates[i].LikelyDestructive, candidates[i].AuthSensitive) != repoTraceHazardRank(candidates[j].WriteCapable, candidates[j].LikelyDestructive, candidates[j].AuthSensitive) {
			return repoTraceHazardRank(candidates[i].WriteCapable, candidates[i].LikelyDestructive, candidates[i].AuthSensitive) > repoTraceHazardRank(candidates[j].WriteCapable, candidates[j].LikelyDestructive, candidates[j].AuthSensitive)
		}
		if candidates[i].Method != candidates[j].Method {
			return candidates[i].Method < candidates[j].Method
		}
		return candidates[i].Action < candidates[j].Action
	})
	for _, form := range candidates {
		hazards := repoTraceHazardTypes(form.WriteCapable, form.LikelyDestructive, form.AuthSensitive)
		fields := strings.Join(form.Fields, ", ")
		if fields == "" {
			fields = "no named fields detected"
		}
		path := repoTraceOwnedPath(form.Action, browserCtx.CurrentURL)
		builder.add(RepoTracePlanStep{
			Kind:        "inspect_form",
			Target:      firstNonEmpty(path, form.Action, browserCtx.CurrentPath),
			Method:      normalizeRepoTraceMethod(form.Method),
			Path:        path,
			Mode:        repoTracePlanMode(hazards, false),
			HazardTypes: hazards,
			Source:      "browser.form",
			Summary:     fmt.Sprintf("Inspect the %s form and its fields (%s) without submitting it.", normalizeRepoTraceMethod(form.Method), fields),
		})
	}
}

func appendRepoTraceControlPlanSteps(builder *repoTracePlanBuilder, controls []RepoTraceControlHint) {
	candidates := append([]RepoTraceControlHint(nil), controls...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if repoTraceHazardRank(candidates[i].WriteCapable, candidates[i].LikelyDestructive, candidates[i].AuthSensitive) != repoTraceHazardRank(candidates[j].WriteCapable, candidates[j].LikelyDestructive, candidates[j].AuthSensitive) {
			return repoTraceHazardRank(candidates[i].WriteCapable, candidates[i].LikelyDestructive, candidates[i].AuthSensitive) > repoTraceHazardRank(candidates[j].WriteCapable, candidates[j].LikelyDestructive, candidates[j].AuthSensitive)
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Label < candidates[j].Label
	})
	for _, control := range candidates {
		if control.RevealSurface || control.InternalNavigation {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		if len(hazards) == 0 {
			continue
		}
		target := firstNonEmpty(control.Label, control.Name, control.Path, control.FormAction)
		builder.add(RepoTracePlanStep{
			Kind:        "review_control",
			Target:      target,
			Method:      normalizeRepoTraceMethod(control.FormMethod),
			Path:        control.Path,
			Mode:        repoTracePlanMode(hazards, false),
			HazardTypes: hazards,
			Source:      "browser.control",
			Summary:     fmt.Sprintf("Review the %s control before any activation.", firstNonEmpty(control.ControlType, control.TagName, "interactive")),
		})
	}
}

// RefreshRepoTraceBrowserContext recollects the bounded browser discovery
// snapshot for a session and returns only the newly exposed surfaces and plan
// steps relative to the previous snapshot.
func (m *SessionManager) RefreshRepoTraceBrowserContext(ctx context.Context, sessionID string, previous RepoTraceBrowserContext, opts *RepoTraceOptions) (RepoTraceContextRefresh, error) {
	if strings.TrimSpace(sessionID) == "" {
		return RepoTraceContextRefresh{}, fmt.Errorf("session_id is required")
	}

	page, ok := m.Page(sessionID)
	if !ok {
		return RepoTraceContextRefresh{}, fmt.Errorf("unknown session: %s", sessionID)
	}

	traceCfg := resolveRepoTraceConfig(m.cfg.RepoTrace, opts)
	if !traceCfg.Enabled {
		return RepoTraceContextRefresh{}, errors.New("repo trace is disabled")
	}

	previous = normalizeRepoTraceBrowserContext(previous, traceCfg.MaxSeedHints, traceCfg.MaxNavigationHints, traceCfg.MaxControlHints)
	current, warnings, err := m.collectRepoTraceBrowserContext(ctx, sessionID, page, traceCfg)
	if err != nil {
		return RepoTraceContextRefresh{}, err
	}

	refresh := buildRepoTraceContextRefresh(previous, current, traceCfg.MaxPlanSteps)
	refresh.Warnings = append(refresh.Warnings, warnings...)
	return refresh, nil
}

func buildRepoTraceContextRefresh(before, after RepoTraceBrowserContext, maxPlanSteps int) RepoTraceContextRefresh {
	refresh := RepoTraceContextRefresh{
		CurrentContext:     after,
		RouteChanged:       repoTraceRouteChanged(before, after),
		SnapshotChanged:    repoTraceSnapshotChanged(before, after),
		NewNavigationLinks: diffRepoTraceNavigationLinks(before.NavigationLinks, after.NavigationLinks),
		NewForms:           diffRepoTraceFormHints(before.Forms, after.Forms),
		NewControls:        diffRepoTraceControlHints(before.Controls, after.Controls),
	}

	builder := newRepoTracePlanBuilder(maxPlanSteps)
	if refresh.SnapshotChanged {
		appendRepoTraceCaptureContextStep(builder, after)
	}
	appendRepoTraceRevealPlanSteps(builder, refresh.NewControls)
	appendRepoTraceNavigationPlanSteps(builder, refresh.NewNavigationLinks)
	appendRepoTraceNavigationControlPlanSteps(builder, refresh.NewControls)
	appendRepoTraceFormPlanSteps(builder, after, refresh.NewForms)
	appendRepoTraceControlPlanSteps(builder, refresh.NewControls)
	refresh.AppendedPlan = builder.steps
	refresh.PlanExpanded = len(refresh.AppendedPlan) > 0
	refresh.Changed = refresh.SnapshotChanged ||
		len(refresh.NewNavigationLinks) > 0 ||
		len(refresh.NewForms) > 0 ||
		len(refresh.NewControls) > 0
	return refresh
}

func repoTraceRouteChanged(before, after RepoTraceBrowserContext) bool {
	return normalizeRepoTracePath(before.CurrentPath) != normalizeRepoTracePath(after.CurrentPath) ||
		strings.TrimSpace(before.CurrentURL) != strings.TrimSpace(after.CurrentURL)
}

func repoTraceSnapshotChanged(before, after RepoTraceBrowserContext) bool {
	return repoTraceRouteChanged(before, after) ||
		strings.TrimSpace(before.Title) != strings.TrimSpace(after.Title) ||
		!repoTraceEqualStringSlice(before.Headings, after.Headings) ||
		!repoTraceEqualStringSlice(before.ComponentNames, after.ComponentNames) ||
		!repoTraceEqualStringSlice(before.Scripts, after.Scripts)
}

func repoTraceEqualStringSlice(a, b []string) bool {
	a = uniqueOrderedStrings(a)
	b = uniqueOrderedStrings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffRepoTraceNavigationLinks(before, after []RepoTraceNavigationLink) []RepoTraceNavigationLink {
	seen := make(map[string]bool, len(before))
	for _, item := range before {
		seen[repoTraceNavigationLinkSignature(item)] = true
	}

	out := make([]RepoTraceNavigationLink, 0, len(after))
	added := make(map[string]bool)
	for _, item := range after {
		key := repoTraceNavigationLinkSignature(item)
		if seen[key] || added[key] {
			continue
		}
		added[key] = true
		out = append(out, item)
	}
	return out
}

func diffRepoTraceFormHints(before, after []RepoTraceFormHint) []RepoTraceFormHint {
	seen := make(map[string]bool, len(before))
	for _, item := range before {
		seen[repoTraceFormHintSignature(item)] = true
	}

	out := make([]RepoTraceFormHint, 0, len(after))
	added := make(map[string]bool)
	for _, item := range after {
		key := repoTraceFormHintSignature(item)
		if seen[key] || added[key] {
			continue
		}
		added[key] = true
		out = append(out, item)
	}
	return out
}

func diffRepoTraceControlHints(before, after []RepoTraceControlHint) []RepoTraceControlHint {
	seen := make(map[string]bool, len(before))
	for _, item := range before {
		seen[repoTraceControlHintSignature(item)] = true
	}

	out := make([]RepoTraceControlHint, 0, len(after))
	added := make(map[string]bool)
	for _, item := range after {
		key := repoTraceControlHintSignature(item)
		if seen[key] || added[key] {
			continue
		}
		added[key] = true
		out = append(out, item)
	}
	return out
}

func repoTraceNavigationLinkSignature(link RepoTraceNavigationLink) string {
	return strings.Join([]string{
		link.LinkType,
		link.Region,
		link.Path,
		link.Href,
	}, "|")
}

func repoTraceFormHintSignature(form RepoTraceFormHint) string {
	return strings.Join([]string{
		normalizeRepoTraceMethod(form.Method),
		normalizeRepoTracePath(form.Action),
		strings.Join(uniqueOrderedStrings(form.Fields), ","),
		strings.Join(uniqueOrderedStrings(form.SubmitLabels), ","),
	}, "|")
}

func repoTraceControlHintSignature(control RepoTraceControlHint) string {
	identity := firstNonEmpty(
		control.Name,
		control.Path,
		control.FormAction,
		control.Href,
		control.AriaControls,
		control.DataHint,
		control.Label,
	)
	return strings.Join([]string{
		control.TagName,
		control.ControlType,
		control.Role,
		control.Region,
		identity,
	}, "|")
}
