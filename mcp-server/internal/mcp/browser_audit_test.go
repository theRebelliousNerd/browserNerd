package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

func TestBrowserAuditToolContract(t *testing.T) {
	tool := &BrowserAuditTool{}
	if tool.Name() != "browser-audit" {
		t.Fatalf("unexpected name: %s", tool.Name())
	}

	schema := tool.InputSchema()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required fields in schema")
	}
	if len(required) != 2 || required[0] != "session_id" || required[1] != "repo_root" {
		t.Fatalf("unexpected required fields: %v", required)
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map in schema")
	}
	if _, ok := props["expand_handles"]; !ok {
		t.Fatalf("expected expand_handles property in schema")
	}
	if _, ok := props["max_repo_matches"]; !ok {
		t.Fatalf("expected max_repo_matches property in schema")
	}
	if _, ok := props["phase"]; !ok {
		t.Fatalf("expected phase property in schema")
	}
	if _, ok := props["resume_handle"]; !ok {
		t.Fatalf("expected resume_handle property in schema")
	}
	if _, ok := props["allow_risky"]; !ok {
		t.Fatalf("expected allow_risky property in schema")
	}
	if _, ok := props["allow_navigation"]; !ok {
		t.Fatalf("expected allow_navigation property in schema")
	}
	if _, ok := props["allow_mutating"]; !ok {
		t.Fatalf("expected allow_mutating property in schema")
	}
	if _, ok := props["allow_destructive"]; !ok {
		t.Fatalf("expected allow_destructive property in schema")
	}
}

func TestBrowserAuditToolValidation(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserAuditTool{engine: engine}
	ctx := context.Background()

	t.Run("missing session_id", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]interface{}{"repo_root": t.TempDir()})
		if err != nil {
			t.Fatalf("execute should not error: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Fatal("expected success=false")
		}
	})

	t.Run("missing repo_root", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]interface{}{"session_id": "s-audit"})
		if err != nil {
			t.Fatalf("execute should not error: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Fatal("expected success=false")
		}
	})
}

func TestBrowserAuditToolFindings(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	sessions := testSessionManagerForProgressive(t, engine)
	tool := &BrowserAuditTool{sessions: sessions, engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "src", "features", "orgs", "useOrganization.ts")
	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		t.Fatalf("mkdir repo fixture: %v", err)
	}
	source := "export const orgUrl = `/api/v1/organizations/${orgId}/full`\n" +
		"export async function loadOrg(orgId: string) { return fetch(orgUrl) }\n"
	if err := os.WriteFile(targetFile, []byte(source), 0o644); err != nil {
		t.Fatalf("write repo fixture: %v", err)
	}

	sessionID := "s-audit"
	now := time.Now()
	if err := engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "current_url",
			Args:      []interface{}{sessionID, "https://app.example.com/network/org-406ventures"},
			Timestamp: now,
		},
		{
			Predicate: "navigation_event",
			Args:      []interface{}{sessionID, "/network/org-406ventures", now.UnixMilli()},
			Timestamp: now,
		},
		{
			Predicate: "net_request",
			Args:      []interface{}{sessionID, "req-404", "GET", "https://api.cross-thread.ai/api/v1/organizations/org-406ventures/full", "", now.UnixMilli()},
			Timestamp: now,
		},
		{
			Predicate: "net_response",
			Args:      []interface{}{sessionID, "req-404", 404, 45, 45},
			Timestamp: now,
		},
		{
			Predicate: "console_event",
			Args:      []interface{}{sessionID, "error", "Cannot read properties of undefined (reading 'name')", now.UnixMilli()},
			Timestamp: now,
		},
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id":           sessionID,
		"repo_root":            repoRoot,
		"view":                 "compact",
		"include_live_observe": false,
	})
	if err != nil {
		t.Fatalf("browser-audit execute failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if !resultMap["success"].(bool) {
		t.Fatalf("expected success=true, got %v", resultMap)
	}
	if resultMap["status"] != "error" {
		t.Fatalf("expected status=error, got %v", resultMap["status"])
	}

	handles, ok := resultMap["evidence_handles"].([]string)
	if !ok {
		t.Fatalf("expected evidence_handles []string, got %T", resultMap["evidence_handles"])
	}
	foundContractHandle := false
	for _, handle := range handles {
		if handle == "audit:"+sessionID+":contract_findings" {
			foundContractHandle = true
			break
		}
	}
	if !foundContractHandle {
		t.Fatalf("expected contract findings handle, got %v", handles)
	}

	data, ok := resultMap["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected compact data map, got %T", resultMap["data"])
	}

	findings := toMapSlice(data["contract_findings"])
	if len(findings) == 0 {
		t.Fatal("expected contract findings")
	}
	if findings[0]["kind"] != "endpoint_drift" {
		t.Fatalf("expected endpoint_drift finding, got %v", findings[0]["kind"])
	}
	files := toStringSlice(findings[0]["candidate_files"])
	if len(files) == 0 {
		t.Fatalf("expected candidate files on finding, got %v", findings[0]["candidate_files"])
	}

	repoMatches := toMapSlice(data["repo_matches"])
	if len(repoMatches) == 0 {
		t.Fatal("expected repo matches")
	}
	if repoMatches[0]["file"] != "src/features/orgs/useOrganization.ts" {
		t.Fatalf("unexpected repo match file: %v", repoMatches[0]["file"])
	}
}

func TestBrowserAuditToolIncludesScopedContractFindings(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserAuditTool{engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	now := time.Now()
	sessionID := "s-audit-mangle"

	if err := engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "current_url",
			Args:      []interface{}{sessionID, "https://app.example.com/settings"},
			Timestamp: now,
		},
		{
			Predicate: "scoped_missing_jwt_or_auth_header",
			Args:      []interface{}{sessionID, "save-settings", "/settings", "req-401", "/api/settings", "POST", "jwt"},
			Timestamp: now,
		},
	}); err != nil {
		t.Fatalf("seed scoped facts: %v", err)
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id":           sessionID,
		"repo_root":            repoRoot,
		"view":                 "compact",
		"include_repo_matches": false,
		"include_live_observe": false,
	})
	if err != nil {
		t.Fatalf("browser-audit execute failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if !resultMap["success"].(bool) {
		t.Fatalf("expected success=true, got %v", resultMap)
	}

	data, ok := resultMap["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected compact data map, got %T", resultMap["data"])
	}

	mangleFindings := toMapSlice(data["mangle_contracts"])
	if len(mangleFindings) == 0 {
		t.Fatal("expected mangle contract findings")
	}
	if mangleFindings[0]["kind"] != "missing_auth" {
		t.Fatalf("expected missing_auth finding, got %v", mangleFindings[0]["kind"])
	}

	combinedFindings := toMapSlice(data["contract_findings"])
	if len(combinedFindings) == 0 {
		t.Fatal("expected combined contract findings")
	}
	if combinedFindings[0]["kind"] != "missing_auth" {
		t.Fatalf("expected combined findings to prioritize missing_auth, got %v", combinedFindings[0]["kind"])
	}
}

func TestBrowserAuditToolDiscoverIsPassive(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	sessions := testSessionManagerForProgressive(t, engine)
	tool := &BrowserAuditTool{sessions: sessions, engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	sessionID := "s-audit-discover"
	now := time.Now()

	if err := engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "current_url",
			Args:      []interface{}{sessionID, "https://app.example.com/settings"},
			Timestamp: now,
		},
		{
			Predicate: "navigation_event",
			Args:      []interface{}{sessionID, "/settings", now.UnixMilli()},
			Timestamp: now,
		},
		{
			Predicate: "net_request",
			Args:      []interface{}{sessionID, "req-401", "POST", "https://api.example.com/api/settings", "", now.UnixMilli()},
			Timestamp: now,
		},
		{
			Predicate: "net_response",
			Args:      []interface{}{sessionID, "req-401", 401, 32, 32},
			Timestamp: now,
		},
	}); err != nil {
		t.Fatalf("seed discover facts: %v", err)
	}

	discoverResult, err := tool.Execute(ctx, map[string]interface{}{
		"session_id":           sessionID,
		"repo_root":            repoRoot,
		"phase":                "discover",
		"view":                 "compact",
		"include_repo_matches": true,
		"include_live_observe": false,
	})
	if err != nil {
		t.Fatalf("discover execute failed: %v", err)
	}

	discoverMap := discoverResult.(map[string]interface{})
	if discoverMap["phase"] != "discover" {
		t.Fatalf("expected phase discover, got %v", discoverMap["phase"])
	}
	if !discoverMap["approval_required"].(bool) {
		t.Fatalf("expected approval_required=true when report phase would emit scoped contract facts")
	}
	if skipped, _ := discoverMap["risky_actions_skipped"].(bool); !skipped {
		t.Fatalf("expected risky_actions_skipped=true")
	}
	data := discoverMap["data"].(map[string]interface{})
	plan := toMapSlice(data["audit_plan"])
	if len(plan) < 4 {
		t.Fatalf("expected audit plan entries, got %v", plan)
	}
	if plan[0]["tool"] != "browser-observe" {
		t.Fatalf("expected first discover step to use browser-observe, got %v", plan[0]["tool"])
	}
	hazards := toMapSlice(data["audit_hazards"])
	if len(hazards) == 0 {
		t.Fatal("expected hazards for discover phase")
	}
	if _, ok := data["repo_matches"]; ok {
		t.Fatalf("discover should not include repo_matches data: %v", data)
	}
	if got := len(engine.FactsByPredicate("scoped_frontend_api_contract")); got != 0 {
		t.Fatalf("discover should not emit scoped_frontend_api_contract facts, got %d", got)
	}
	reportHandles := toStringSlice(discoverMap["report_handles"])
	if len(reportHandles) == 0 {
		t.Fatalf("expected report handles in discover response")
	}
	foundReportHandle := false
	for _, handle := range reportHandles {
		if handle == "audit:"+sessionID+":contract_findings" {
			foundReportHandle = true
			break
		}
	}
	if !foundReportHandle {
		t.Fatalf("expected contract findings report handle, got %v", reportHandles)
	}

	persisted, err := loadPersistedAuditRun(repoRoot, sessionID, fmt.Sprintf("%v", discoverMap["audit_id"]))
	if err != nil {
		t.Fatalf("load persisted discover run: %v", err)
	}
	if persisted.Phase != "discover" {
		t.Fatalf("expected persisted discover phase, got %s", persisted.Phase)
	}
	if persisted.Status != "planned" {
		t.Fatalf("expected persisted planned status, got %s", persisted.Status)
	}
	if len(persisted.Plan) != len(plan) {
		t.Fatalf("expected persisted plan length %d, got %d", len(plan), len(persisted.Plan))
	}
	if len(engine.FactsByPredicate("audit_plan_step")) == 0 {
		t.Fatal("expected discover to emit audit_plan_step facts")
	}
}

func TestBrowserAuditToolExecuteRunsSafeStepsAndSkipsRiskyOnes(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserAuditTool{engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	sessionID := "s-audit-execute"
	now := time.Now()

	if err := engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "current_url",
			Args:      []interface{}{sessionID, "https://app.example.com/settings"},
			Timestamp: now,
		},
	}); err != nil {
		t.Fatalf("seed execute facts: %v", err)
	}

	run := persistedAuditRun{
		AuditID:          "audit-exec-1",
		SessionID:        sessionID,
		Phase:            "discover",
		RepoRoot:         repoRoot,
		CurrentURL:       "https://app.example.com/settings",
		CurrentPath:      "/settings",
		GeneratedAt:      now.UnixMilli(),
		ApprovalRequired: true,
		Plan: []map[string]interface{}{
			{
				"step":   1,
				"id":     "report-contracts",
				"tool":   "browser-audit",
				"risky":  false,
				"status": "ready",
				"args": map[string]interface{}{
					"session_id":           sessionID,
					"repo_root":            repoRoot,
					"phase":                "report",
					"view":                 "summary",
					"include_repo_matches": false,
					"include_live_observe": false,
				},
			},
			{
				"step":   2,
				"id":     "repo-heavy-report",
				"tool":   "browser-audit",
				"risky":  true,
				"status": "ready",
				"args": map[string]interface{}{
					"session_id":           sessionID,
					"repo_root":            repoRoot,
					"phase":                "report",
					"view":                 "summary",
					"include_repo_matches": true,
					"include_live_observe": false,
				},
			},
		},
		Hazards: []map[string]interface{}{
			{
				"hazard":   "scoped_fact_emission",
				"severity": "high",
				"step":     2,
			},
		},
		Handles: []string{"audit:" + sessionID + ":contract_findings"},
	}

	if err := persistAuditRun(repoRoot, run); err != nil {
		t.Fatalf("persist audit run: %v", err)
	}
	auditPath := filepath.Join(repoRoot, ".browsernerd", "data", "audits", run.AuditID+".json")
	if info, err := os.Stat(auditPath); err != nil {
		t.Fatalf("stat persisted audit: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("persisted audit is accessible outside its owner: mode=%o", info.Mode().Perm())
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id": sessionID,
		"repo_root":  repoRoot,
		"phase":      "execute",
		"audit_id":   run.AuditID,
		"view":       "compact",
	})
	if err != nil {
		t.Fatalf("execute phase failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if resultMap["phase"] != "execute" {
		t.Fatalf("expected phase execute, got %v", resultMap["phase"])
	}
	if resultMap["approval_required"].(bool) != true {
		t.Fatalf("expected approval_required=true after skipping risky step")
	}

	completed := toMapSlice(resultMap["completed_steps"])
	if len(completed) < 1 {
		t.Fatalf("expected completed safe steps, got %+v", completed)
	}
	if completed[0]["id"] != "report-contracts" {
		t.Fatalf("expected initial completed step to be report-contracts, got %+v", completed)
	}
	if _, ok := completed[0]["evidence"]; !ok {
		t.Fatalf("expected completed step evidence, got %+v", completed[0])
	}
	completedIDs := make(map[string]bool, len(completed))
	for _, step := range completed {
		completedIDs[fmt.Sprintf("%v", step["id"])] = true
	}
	for _, expectedID := range []string{"report-contracts", "report_contracts", "reason_findings", "resume_focus"} {
		if !completedIDs[expectedID] {
			t.Fatalf("expected recursive execute to complete %s, got %+v", expectedID, completed)
		}
	}
	if rediscovered, ok := completed[0]["evidence"].(map[string]interface{})["rediscovered_steps"]; !ok || len(toStringSlice(rediscovered)) == 0 {
		t.Fatalf("expected initial completed step to record rediscovered steps, got %+v", completed[0])
	}
	skipped := toMapSlice(resultMap["skipped_steps"])
	if len(skipped) != 1 || skipped[0]["id"] != "repo-heavy-report" {
		t.Fatalf("expected exactly one skipped risky step, got %+v", skipped)
	}

	completedFacts := engine.FactsByPredicate("audit_plan_step_completed")
	if len(completedFacts) == 0 {
		t.Fatal("expected audit_plan_step_completed facts")
	}
	skippedFacts := engine.FactsByPredicate("audit_plan_step_skipped")
	if len(skippedFacts) == 0 {
		t.Fatal("expected audit_plan_step_skipped facts")
	}
	if len(engine.FactsByPredicate("scoped_audit_run_completed_action")) == 0 {
		t.Fatal("expected scoped_audit_run_completed_action facts")
	}
	if len(engine.FactsByPredicate("scoped_audit_run_skipped_action")) == 0 {
		t.Fatal("expected scoped_audit_run_skipped_action facts")
	}

	persisted, err := loadPersistedAuditRun(repoRoot, sessionID, run.AuditID)
	if err != nil {
		t.Fatalf("load persisted execute run: %v", err)
	}
	if persisted.Phase != "execute" {
		t.Fatalf("expected persisted execute phase, got %s", persisted.Phase)
	}
	if persisted.Status != "resume_ready" {
		t.Fatalf("expected persisted resume_ready status, got %s", persisted.Status)
	}
	if len(persisted.CompletedSteps) != len(completed) {
		t.Fatalf("expected persisted completed step count %d, got %+v", len(completed), persisted.CompletedSteps)
	}
	if len(persisted.SkippedSteps) != 1 {
		t.Fatalf("expected one persisted skipped step, got %+v", persisted.SkippedSteps)
	}
}

func TestBrowserAuditToolExecuteAllowsMutatingActionWithExplicitFlag(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserAuditTool{engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	sessionID := "s-audit-execute-allow"
	now := time.Now()

	run := persistedAuditRun{
		AuditID:     "audit-exec-allow-1",
		SessionID:   sessionID,
		Phase:       "discover",
		RepoRoot:    repoRoot,
		CurrentURL:  "https://app.example.com/settings",
		CurrentPath: "/settings",
		GeneratedAt: now.UnixMilli(),
		Plan: []map[string]interface{}{
			{
				"step":       1,
				"id":         "click-danger-button",
				"action":     "click",
				"ref":        "danger-button",
				"route":      "/settings",
				"api_route":  "/api/settings/profile",
				"request_id": "req-danger",
				"status":     "ready",
			},
		},
		Handles: []string{"audit:" + sessionID + ":contract_findings"},
	}

	if err := persistAuditRun(repoRoot, run); err != nil {
		t.Fatalf("persist audit run: %v", err)
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id": sessionID,
		"repo_root":  repoRoot,
		"phase":      "execute",
		"audit_id":   run.AuditID,
		"view":       "compact",
	})
	if err != nil {
		t.Fatalf("execute without allow_mutating failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if completed := toMapSlice(resultMap["completed_steps"]); len(completed) != 0 {
		t.Fatalf("expected no completed steps without allow_mutating, got %+v", completed)
	}
	skipped := toMapSlice(resultMap["skipped_steps"])
	if len(skipped) != 1 {
		t.Fatalf("expected one skipped step without allow_mutating, got %+v", skipped)
	}
	if skipped[0]["reason"] != "requires explicit allow_mutating" {
		t.Fatalf("expected allow_mutating skip reason, got %+v", skipped[0])
	}

	runAllowed := persistedAuditRun{
		AuditID:     "audit-exec-allow-2",
		SessionID:   sessionID,
		Phase:       "discover",
		RepoRoot:    repoRoot,
		CurrentURL:  "https://app.example.com/settings",
		CurrentPath: "/settings",
		GeneratedAt: now.UnixMilli(),
		Plan: []map[string]interface{}{
			{
				"step":       1,
				"id":         "click-danger-button",
				"action":     "click",
				"ref":        "danger-button",
				"route":      "/settings",
				"api_route":  "/api/settings/profile",
				"request_id": "req-danger",
				"status":     "ready",
			},
		},
		Handles: []string{"audit:" + sessionID + ":contract_findings"},
	}
	if err := persistAuditRun(repoRoot, runAllowed); err != nil {
		t.Fatalf("persist allowed audit run: %v", err)
	}

	result, err = tool.Execute(ctx, map[string]interface{}{
		"session_id":     sessionID,
		"repo_root":      repoRoot,
		"phase":          "execute",
		"audit_id":       runAllowed.AuditID,
		"allow_mutating": true,
		"view":           "compact",
	})
	if err != nil {
		t.Fatalf("execute with allow_mutating failed: %v", err)
	}

	resultMap = result.(map[string]interface{})
	completed := toMapSlice(resultMap["completed_steps"])
	if len(completed) != 1 {
		t.Fatalf("expected one completed step with allow_mutating, got %+v", completed)
	}
	if completed[0]["action"] != "click" {
		t.Fatalf("expected click action in completed step, got %+v", completed[0])
	}

	foundClickCompletion := false
	for _, fact := range engine.FactsByPredicate("scoped_audit_run_completed_action") {
		if len(fact.Args) >= 3 && fact.Args[1] == runAllowed.AuditID && fact.Args[2] == "click" {
			foundClickCompletion = true
			break
		}
	}
	if !foundClickCompletion {
		t.Fatal("expected click scoped_audit_run_completed_action fact")
	}
}

func TestAppendRecursiveAuditStepsDedupesAndAppendsRepoTracePlan(t *testing.T) {
	initial := []map[string]interface{}{
		{
			"step":   1,
			"id":     "observe_context",
			"tool":   "browser-observe",
			"action": "reveal_page_context",
			"ref":    "page_context",
			"route":  "/root",
			"path":   "",
		},
	}

	report := &browser.RepoTraceReport{
		AuditPlan: []browser.RepoTracePlanStep{
			{
				ID:          "step-1",
				Kind:        "map_navigation",
				Target:      "Admin",
				Path:        "/admin",
				Mode:        "read_only",
				HazardTypes: nil,
				Source:      "browser.navigation_link",
				Summary:     "Review the admin navigation target.",
			},
			{
				ID:          "step-2",
				Kind:        "map_navigation",
				Target:      "Admin",
				Path:        "/admin",
				Mode:        "read_only",
				HazardTypes: nil,
				Source:      "browser.navigation_link",
				Summary:     "Review the admin navigation target.",
			},
			{
				ID:          "step-3",
				Kind:        "inspect_form",
				Target:      "/admin/users",
				Method:      "POST",
				Path:        "/admin/users",
				Mode:        "inspect_only",
				HazardTypes: []string{"write_capable"},
				Source:      "browser.form",
				Summary:     "Inspect the user form.",
			},
		},
	}

	merged := appendRecursiveAuditSteps(initial, "session-recursive", report)
	if len(merged) != 3 {
		t.Fatalf("expected 3 total steps after deduped append, got %+v", merged)
	}
	if merged[1]["action"] != "reveal_navigation_target" {
		t.Fatalf("expected appended navigation action, got %+v", merged[1])
	}
	if merged[2]["action"] != "reveal_form_contract" {
		t.Fatalf("expected inspect_form to become a non-mutating contract reveal, got %+v", merged[2])
	}
	if merged[2]["hazard_class"] != "non_mutating_reveal" {
		t.Fatalf("expected non-mutating reveal hazard class on inspect_form-derived step, got %+v", merged[2])
	}
}

func TestBrowserAuditToolResumeNarrowsSectionsByHandle(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserAuditTool{engine: engine}
	ctx := context.Background()

	repoRoot := t.TempDir()
	sessionID := "s-audit-resume"
	now := time.Now()

	if err := engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "current_url",
			Args:      []interface{}{sessionID, "https://app.example.com/settings"},
			Timestamp: now,
		},
		{
			Predicate: "scoped_missing_jwt_or_auth_header",
			Args:      []interface{}{sessionID, "save-settings", "/settings", "req-401", "/api/settings", "POST", "jwt"},
			Timestamp: now,
		},
	}); err != nil {
		t.Fatalf("seed resume facts: %v", err)
	}

	resumeHandle := "audit:" + sessionID + ":mangle_contracts"
	resumeResult, err := tool.Execute(ctx, map[string]interface{}{
		"session_id":           sessionID,
		"repo_root":            repoRoot,
		"phase":                "resume",
		"view":                 "compact",
		"resume_handle":        resumeHandle,
		"include_repo_matches": false,
		"include_live_observe": false,
	})
	if err != nil {
		t.Fatalf("resume execute failed: %v", err)
	}

	resumeMap := resumeResult.(map[string]interface{})
	if resumeMap["phase"] != "resume" {
		t.Fatalf("expected phase resume, got %v", resumeMap["phase"])
	}
	resumeHandles := toStringSlice(resumeMap["resume_handles"])
	if len(resumeHandles) != 1 || resumeHandles[0] != resumeHandle {
		t.Fatalf("expected focused resume handle %q, got %v", resumeHandle, resumeHandles)
	}

	resumeData := resumeMap["data"].(map[string]interface{})
	if len(resumeData) != 1 {
		t.Fatalf("expected exactly one resumed section, got %v", resumeData)
	}
	mangleFindings := toMapSlice(resumeData["mangle_contracts"])
	if len(mangleFindings) == 0 {
		t.Fatalf("expected mangle_contracts data in resume response")
	}
	if mangleFindings[0]["kind"] != "missing_auth" {
		t.Fatalf("expected missing_auth finding, got %v", mangleFindings[0]["kind"])
	}
}
