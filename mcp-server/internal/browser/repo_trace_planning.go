package browser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func buildRepoTraceSeedSet(browserCtx RepoTraceBrowserContext, maxSeeds int) repoTraceSeedSet {
	if maxSeeds <= 0 {
		maxSeeds = defaultRepoTraceMaxSeedHints
	}
	set := repoTraceSeedSet{
		pageTokens:      make(map[string]bool),
		componentTokens: make(map[string]bool),
		fieldTokens:     make(map[string]bool),
		seeds:           make([]RepoTraceSeed, 0, maxSeeds),
	}
	seenSeeds := make(map[string]bool)

	addSeed := func(kind, value, source string) {
		value = strings.TrimSpace(value)
		if value == "" || len(set.seeds) >= maxSeeds {
			return
		}
		key := kind + "|" + value + "|" + source
		if seenSeeds[key] {
			return
		}
		seenSeeds[key] = true
		set.seeds = append(set.seeds, RepoTraceSeed{Kind: kind, Value: value, Source: source})
	}

	currentPath := normalizeRepoTracePath(browserCtx.CurrentPath)
	if currentPath != "" {
		set.pagePaths = append(set.pagePaths, currentPath)
		addSeed("page_path", currentPath, "browser.location")
	}

	addRepoTraceTokens(set.pageTokens, browserCtx.Title)
	for _, heading := range browserCtx.Headings {
		addSeed("heading", heading, "dom.heading")
		addRepoTraceTokens(set.pageTokens, heading)
	}
	for _, component := range browserCtx.ComponentNames {
		addSeed("component", component, "react.fiber")
		addRepoTraceTokens(set.componentTokens, component)
	}
	for _, form := range browserCtx.Forms {
		actionPath := repoTraceOwnedPath(form.Action, browserCtx.CurrentURL)
		if actionPath != "" {
			set.apiPaths = append(set.apiPaths, actionPath)
			addSeed("form_action", actionPath, "dom.form")
		}
		if form.Method != "" {
			addSeed("form_method", normalizeRepoTraceMethod(form.Method), "dom.form")
		}
		for _, field := range form.Fields {
			addSeed("form_field", field, "dom.form")
			addRepoTraceTokens(set.fieldTokens, field)
		}
		for _, submitLabel := range form.SubmitLabels {
			addSeed("form_submit_label", submitLabel, "dom.form")
			addRepoTraceTokens(set.fieldTokens, submitLabel)
		}
	}
	for _, link := range browserCtx.NavigationLinks {
		if link.InternalCandidate && link.Path != "" {
			set.pagePaths = append(set.pagePaths, link.Path)
			addSeed("nav_path", link.Path, "dom.navigation")
		}
		if link.Label != "" && (link.InternalCandidate || link.AuthSensitive) {
			addSeed("nav_label", link.Label, "dom.navigation")
			addRepoTraceTokens(set.pageTokens, link.Label)
		}
	}
	for _, control := range browserCtx.Controls {
		targetPath := firstNonEmpty(control.Path, repoTraceOwnedPath(control.FormAction, browserCtx.CurrentURL), repoTraceOwnedPath(control.Href, browserCtx.CurrentURL))
		if targetPath != "" {
			if strings.Contains(targetPath, "/api/") || repoTraceWriteMethods[normalizeRepoTraceMethod(control.FormMethod)] {
				set.apiPaths = append(set.apiPaths, targetPath)
			} else {
				set.pagePaths = append(set.pagePaths, targetPath)
			}
			addSeed("control_path", targetPath, "dom.control")
			if control.InternalNavigation {
				addSeed("control_nav_path", targetPath, "dom.control")
			}
		}
		if control.Name != "" {
			addSeed("control_name", control.Name, "dom.control")
			if control.InternalNavigation || control.RevealSurface {
				addRepoTraceTokens(set.pageTokens, control.Name)
			} else {
				addRepoTraceTokens(set.fieldTokens, control.Name)
			}
		}
		if control.Label != "" {
			addSeed("control_label", control.Label, "dom.control")
			if control.RevealSurface {
				addSeed("reveal_label", control.Label, "dom.control")
			}
			if control.InternalNavigation || control.RevealSurface {
				addRepoTraceTokens(set.pageTokens, control.Label)
			} else {
				addRepoTraceTokens(set.fieldTokens, control.Label)
			}
		}
	}
	for _, req := range browserCtx.Requests {
		path := normalizeRepoTracePath(req.Path)
		if path == "" {
			path = normalizeRepoTracePath(req.URL)
		}
		if path == "" {
			continue
		}
		set.apiPaths = append(set.apiPaths, path)
		addSeed("request_path", path, "network.request")
		if req.Method != "" {
			addSeed("request_method", strings.ToUpper(req.Method), "network.request")
		}
	}
	for _, script := range browserCtx.Scripts {
		base := filepath.Base(script)
		addSeed("script", base, "dom.script")
		addRepoTraceTokens(set.pageTokens, base)
	}

	set.pagePaths = uniqueOrderedStrings(set.pagePaths)
	set.apiPaths = uniqueOrderedStrings(set.apiPaths)
	return set
}

func summarizeRepoTraceHazards(browserCtx RepoTraceBrowserContext) RepoTraceHazardSummary {
	summary := RepoTraceHazardSummary{
		NavigationLinks: len(browserCtx.NavigationLinks),
	}
	for _, form := range browserCtx.Forms {
		if form.WriteCapable {
			summary.WriteCapableForms++
		}
		if form.LikelyDestructive {
			summary.DestructiveForms++
		}
		if form.AuthSensitive {
			summary.AuthSensitiveForms++
		}
	}
	for _, control := range browserCtx.Controls {
		if control.InternalNavigation {
			summary.InternalNavigationControls++
		}
		if control.RevealSurface {
			summary.RevealControls++
		}
		if control.WriteCapable {
			summary.WriteCapableControls++
		}
		if control.LikelyDestructive {
			summary.DestructiveControls++
		}
		if control.AuthSensitive {
			summary.AuthSensitiveControls++
		}
	}
	for _, link := range browserCtx.NavigationLinks {
		if link.InternalCandidate {
			summary.InternalNavigationLinks++
		}
		if link.AuthSensitive {
			summary.AuthSensitiveNavigationLinks++
		}
	}
	return summary
}

func buildRepoTraceAuditPlan(browserCtx RepoTraceBrowserContext, maxSteps int) []RepoTracePlanStep {
	if maxSteps <= 0 {
		maxSteps = defaultRepoTraceMaxPlanSteps
	}
	steps := make([]RepoTracePlanStep, 0, maxSteps)
	seen := make(map[string]bool)
	appendStep := func(step RepoTracePlanStep) {
		if len(steps) >= maxSteps {
			return
		}
		key := strings.Join([]string{step.Kind, step.Target, step.Method, step.Path, step.Mode, step.Source}, "|")
		if seen[key] {
			return
		}
		seen[key] = true
		step.ID = fmt.Sprintf("step-%d", len(steps)+1)
		steps = append(steps, step)
	}

	appendStep(RepoTracePlanStep{
		Kind:    "capture_context",
		Target:  firstNonEmpty(browserCtx.CurrentPath, "/"),
		Path:    firstNonEmpty(browserCtx.CurrentPath, "/"),
		Mode:    "read_only",
		Source:  "browser.context",
		Summary: "Capture the current route, headings, and component hints before inspecting higher-risk surfaces.",
	})

	revealControls := append([]RepoTraceControlHint(nil), browserCtx.Controls...)
	sort.SliceStable(revealControls, func(i, j int) bool {
		if revealControls[i].AuthSensitive != revealControls[j].AuthSensitive {
			return revealControls[i].AuthSensitive
		}
		if revealControls[i].Region != revealControls[j].Region {
			return revealControls[i].Region < revealControls[j].Region
		}
		if revealControls[i].RevealKind != revealControls[j].RevealKind {
			return revealControls[i].RevealKind < revealControls[j].RevealKind
		}
		return revealControls[i].Label < revealControls[j].Label
	})
	for _, control := range revealControls {
		if !control.RevealSurface {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		target := firstNonEmpty(control.Label, control.Name, control.AriaControls, control.Path)
		appendStep(RepoTracePlanStep{
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

	navLinks := append([]RepoTraceNavigationLink(nil), browserCtx.NavigationLinks...)
	sort.SliceStable(navLinks, func(i, j int) bool {
		if navLinks[i].InternalCandidate != navLinks[j].InternalCandidate {
			return navLinks[i].InternalCandidate
		}
		if navLinks[i].AuthSensitive != navLinks[j].AuthSensitive {
			return navLinks[i].AuthSensitive
		}
		if navLinks[i].Region != navLinks[j].Region {
			return navLinks[i].Region < navLinks[j].Region
		}
		if navLinks[i].Path != navLinks[j].Path {
			return navLinks[i].Path < navLinks[j].Path
		}
		return navLinks[i].Label < navLinks[j].Label
	})
	for _, link := range navLinks {
		if !link.InternalCandidate {
			continue
		}
		hazards := repoTraceHazardTypes(false, false, link.AuthSensitive)
		target := firstNonEmpty(link.Label, link.Path, link.Href)
		appendStep(RepoTracePlanStep{
			Kind:        "map_navigation",
			Target:      target,
			Path:        link.Path,
			Mode:        repoTracePlanMode(hazards, false),
			HazardTypes: hazards,
			Source:      "browser.navigation_link",
			Summary:     fmt.Sprintf("Review the %s internal route candidate before deeper repo recursion.", firstNonEmpty(link.Region, "content")),
		})
	}

	navControls := append([]RepoTraceControlHint(nil), browserCtx.Controls...)
	sort.SliceStable(navControls, func(i, j int) bool {
		if navControls[i].AuthSensitive != navControls[j].AuthSensitive {
			return navControls[i].AuthSensitive
		}
		if navControls[i].Region != navControls[j].Region {
			return navControls[i].Region < navControls[j].Region
		}
		if navControls[i].Path != navControls[j].Path {
			return navControls[i].Path < navControls[j].Path
		}
		return navControls[i].Label < navControls[j].Label
	})
	for _, control := range navControls {
		if !control.InternalNavigation {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		target := firstNonEmpty(control.Label, control.Name, control.Path, control.Href)
		appendStep(RepoTracePlanStep{
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

	forms := append([]RepoTraceFormHint(nil), browserCtx.Forms...)
	sort.SliceStable(forms, func(i, j int) bool {
		if repoTraceHazardRank(forms[i].WriteCapable, forms[i].LikelyDestructive, forms[i].AuthSensitive) != repoTraceHazardRank(forms[j].WriteCapable, forms[j].LikelyDestructive, forms[j].AuthSensitive) {
			return repoTraceHazardRank(forms[i].WriteCapable, forms[i].LikelyDestructive, forms[i].AuthSensitive) > repoTraceHazardRank(forms[j].WriteCapable, forms[j].LikelyDestructive, forms[j].AuthSensitive)
		}
		if forms[i].Method != forms[j].Method {
			return forms[i].Method < forms[j].Method
		}
		return forms[i].Action < forms[j].Action
	})
	for _, form := range forms {
		hazards := repoTraceHazardTypes(form.WriteCapable, form.LikelyDestructive, form.AuthSensitive)
		fields := strings.Join(form.Fields, ", ")
		if fields == "" {
			fields = "no named fields detected"
		}
		path := repoTraceOwnedPath(form.Action, browserCtx.CurrentURL)
		appendStep(RepoTracePlanStep{
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

	controls := append([]RepoTraceControlHint(nil), browserCtx.Controls...)
	sort.SliceStable(controls, func(i, j int) bool {
		if repoTraceHazardRank(controls[i].WriteCapable, controls[i].LikelyDestructive, controls[i].AuthSensitive) != repoTraceHazardRank(controls[j].WriteCapable, controls[j].LikelyDestructive, controls[j].AuthSensitive) {
			return repoTraceHazardRank(controls[i].WriteCapable, controls[i].LikelyDestructive, controls[i].AuthSensitive) > repoTraceHazardRank(controls[j].WriteCapable, controls[j].LikelyDestructive, controls[j].AuthSensitive)
		}
		if controls[i].Path != controls[j].Path {
			return controls[i].Path < controls[j].Path
		}
		return controls[i].Label < controls[j].Label
	})
	for _, control := range controls {
		if control.RevealSurface || control.InternalNavigation {
			continue
		}
		hazards := repoTraceHazardTypes(control.WriteCapable, control.LikelyDestructive, control.AuthSensitive)
		if len(hazards) == 0 {
			continue
		}
		target := firstNonEmpty(control.Label, control.Name, control.Path, control.FormAction)
		appendStep(RepoTracePlanStep{
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

	return steps
}

func repoTraceHazardRank(writeCapable, destructive, authSensitive bool) int {
	score := 0
	if writeCapable {
		score++
	}
	if authSensitive {
		score += 2
	}
	if destructive {
		score += 4
	}
	return score
}

func repoTraceHazardTypes(writeCapable, destructive, authSensitive bool) []string {
	kinds := make([]string, 0, 3)
	if authSensitive {
		kinds = append(kinds, "auth_sensitive")
	}
	if destructive {
		kinds = append(kinds, "likely_destructive")
	}
	if writeCapable {
		kinds = append(kinds, "write_capable")
	}
	return kinds
}

func repoTracePlanMode(hazards []string, revealOnly bool) string {
	for _, hazard := range hazards {
		if hazard == "likely_destructive" {
			return "confirm_before_mutation"
		}
	}
	for _, hazard := range hazards {
		if hazard == "write_capable" || hazard == "auth_sensitive" {
			return "inspect_only"
		}
	}
	if revealOnly {
		return "reveal_only"
	}
	return "read_only"
}
