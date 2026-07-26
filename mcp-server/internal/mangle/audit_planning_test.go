package mangle

import (
	"context"
	"testing"
)

func queryAuditResults(t *testing.T, engine *Engine, query string) []QueryResult {
	t.Helper()

	results, err := engine.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("Query(%q) failed: %v", query, err)
	}

	return results
}

func requireQueryRow(t *testing.T, results []QueryResult, expected map[string]interface{}) {
	t.Helper()

	for _, result := range results {
		match := true
		for key, want := range expected {
			got, ok := result[key]
			if !ok || !valuesEquivalent(got, want) {
				match = false
				break
			}
		}
		if match {
			return
		}
	}

	t.Fatalf("expected query row %v, got %+v", expected, results)
}

func TestAuditPlanningDeterministicActionsAndSafeHazards(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(900, "scoped_frontend_api_contract", "session-plan", "save-settings", "/settings", "/api/settings", "POST", "jwt"),
		contractFact(900, "scoped_backend_api_contract", "session-plan", "/api/settings", "POST", "jwt"),
		contractFact(950, "current_url", "session-plan", "/settings"),
		contractFact(1000, "user_click", "session-plan", "save-settings", int64(1000)),
		contractFact(1100, "net_request", "session-plan", "req-auth", "POST", "/api/settings", "fetch", int64(1100)),
		contractFact(1200, "scoped_frontend_api_contract", "session-plan", "load-audit", "/admin", "/api/admin/audit", "GET", "jwt"),

		contractFact(900, "scoped_frontend_api_contract", "session-plan-clean", "save-settings", "/settings", "/api/settings", "POST", "jwt"),
		contractFact(900, "scoped_backend_api_contract", "session-plan-clean", "/api/settings", "POST", "jwt"),
		contractFact(950, "current_url", "session-plan-clean", "/settings"),
		contractFact(1000, "user_click", "session-plan-clean", "save-settings", int64(1000)),
		contractFact(1100, "net_request", "session-plan-clean", "req-auth-clean", "POST", "/api/settings", "fetch", int64(1100)),
		contractFact(1110, "net_header", "session-plan-clean", "req-auth-clean", "request", "authorization", "Bearer primary-token"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	planRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_plan_action("session-plan", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).`,
	)

	requireQueryRow(t, planRows, map[string]interface{}{
		"ActionKind": "reveal_request_auth",
		"TargetRef":  "req-auth",
		"PageRoute":  "/settings",
		"ApiRoute":   "/api/settings",
		"RequestId":  "req-auth",
		"Priority":   int64(100),
		"Reason":     "missing_auth",
	})

	requireQueryRow(t, planRows, map[string]interface{}{
		"ActionKind": "reveal_frontend_contract",
		"TargetRef":  "load-audit",
		"PageRoute":  "/admin",
		"ApiRoute":   "/api/admin/audit",
		"RequestId":  "",
		"Priority":   int64(94),
		"Reason":     "backend_contract_gap",
	})

	requireQueryRow(t, planRows, map[string]interface{}{
		"ActionKind": "navigate_page_route",
		"TargetRef":  "/admin",
		"PageRoute":  "/admin",
		"ApiRoute":   "/api/admin/audit",
		"RequestId":  "",
		"Priority":   int64(70),
		"Reason":     "replay_contract_gap",
	})

	cleanRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_plan_action("session-plan-clean", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).`,
	)
	if len(cleanRows) != 0 {
		t.Fatalf("expected no scoped audit plan rows for clean session, got %+v", cleanRows)
	}

	hazardRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_plan_hazard("session-plan", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, MutabilityClass, HazardClass, Severity, HazardReason).`,
	)

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "reveal_request_auth",
		"TargetRef":       "req-auth",
		"MutabilityClass": "non_mutating",
		"HazardClass":     "non_mutating_reveal",
		"Severity":        "low",
		"HazardReason":    "reveal_existing_evidence",
	})

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "navigate_page_route",
		"TargetRef":       "/admin",
		"MutabilityClass": "non_mutating",
		"HazardClass":     "non_mutating_navigation",
		"Severity":        "medium",
		"HazardReason":    "replay_route_without_form_mutation",
	})
}

func TestAuditPlanningProposedActionHazards(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(1000, "scoped_audit_proposed_action", "session-proposal", "run-proposal", "reveal_request_payload", "orderId", "/checkout", "/api/orders", "req-order", "inspect missing required field"),
		contractFact(1010, "scoped_audit_proposed_action", "session-proposal", "run-proposal", "navigate_page_route", "/admin", "/admin", "/api/admin/audit", "", "replay current audit route"),
		contractFact(1020, "scoped_audit_proposed_action", "session-proposal", "run-proposal", "type", "email-input", "/login", "/api/login", "", "fill login form"),
		contractFact(1030, "scoped_audit_proposed_action", "session-proposal", "run-proposal", "confirm_delete", "danger-zone", "/settings", "/api/settings/profile", "", "attempt destructive cleanup"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	hazardRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_proposed_action_hazard("session-proposal", "run-proposal", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, MutabilityClass, HazardClass, Severity, HazardReason).`,
	)

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "reveal_request_payload",
		"TargetRef":       "orderId",
		"MutabilityClass": "non_mutating",
		"HazardClass":     "non_mutating_reveal",
		"Severity":        "low",
	})

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "navigate_page_route",
		"TargetRef":       "/admin",
		"MutabilityClass": "non_mutating",
		"HazardClass":     "non_mutating_navigation",
		"Severity":        "medium",
	})

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "type",
		"TargetRef":       "email-input",
		"MutabilityClass": "mutating",
		"HazardClass":     "write_hazard",
		"Severity":        "high",
	})

	requireQueryRow(t, hazardRows, map[string]interface{}{
		"ActionKind":      "confirm_delete",
		"TargetRef":       "danger-zone",
		"MutabilityClass": "mutating",
		"HazardClass":     "destructive_hazard",
		"Severity":        "critical",
	})
}

func TestAuditPlanningRunResumeState(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(900, "scoped_missing_jwt_or_auth_header", "session-resume", "save-settings", "/settings", "req-auth", "/api/settings", "POST", "jwt"),
		contractFact(910, "scoped_frontend_backend_contract_gap", "session-resume", "load-audit", "/admin", "/api/admin/audit", "GET", "backend_contract", "declared", "missing"),

		contractFact(1000, "scoped_audit_run", "session-resume", "run-resume", "contract_audit", int64(1000)),
		contractFact(1010, "scoped_audit_run_completed_action", "session-resume", "run-resume", "reveal_request_auth", "req-auth", "/settings", "/api/settings", "req-auth", int64(1010)),
		contractFact(1020, "scoped_audit_run_skipped_action", "session-resume", "run-resume", "reveal_frontend_contract", "load-audit", "/admin", "/api/admin/audit", "", "already_reviewed", int64(1020)),

		contractFact(1050, "scoped_audit_run", "session-resume", "run-pending", "contract_audit", int64(1050)),
		contractFact(1055, "scoped_audit_run_completed_action", "session-resume", "run-pending", "type", "email-input", "/login", "/api/login", "", int64(1055)),

		contractFact(1070, "scoped_audit_run", "session-resume", "run-skipped", "contract_audit", int64(1070)),
		contractFact(1071, "scoped_audit_run_skipped_action", "session-resume", "run-skipped", "reveal_request_auth", "req-auth", "/settings", "/api/settings", "req-auth", "already_reviewed", int64(1071)),
		contractFact(1072, "scoped_audit_run_skipped_action", "session-resume", "run-skipped", "reveal_frontend_contract", "load-audit", "/admin", "/api/admin/audit", "", "already_reviewed", int64(1072)),
		contractFact(1073, "scoped_audit_run_skipped_action", "session-resume", "run-skipped", "navigate_page_route", "/admin", "/admin", "/api/admin/audit", "", "already_reviewed", int64(1073)),

		contractFact(1100, "scoped_audit_run", "session-resume", "run-complete", "contract_audit", int64(1100)),
		contractFact(1110, "scoped_audit_run_completed_action", "session-resume", "run-complete", "reveal_request_auth", "req-auth", "/settings", "/api/settings", "req-auth", int64(1110)),
		contractFact(1120, "scoped_audit_run_completed_action", "session-resume", "run-complete", "reveal_frontend_contract", "load-audit", "/admin", "/api/admin/audit", "", int64(1120)),
		contractFact(1130, "scoped_audit_run_completed_action", "session-resume", "run-complete", "navigate_page_route", "/admin", "/admin", "/api/admin/audit", "", int64(1130)),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	actionStateRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_action_state("session-resume", "run-resume", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, State).`,
	)

	if len(actionStateRows) != 3 {
		t.Fatalf("expected exactly three action states for partial run, got %+v", actionStateRows)
	}

	requireQueryRow(t, actionStateRows, map[string]interface{}{
		"ActionKind": "reveal_request_auth",
		"TargetRef":  "req-auth",
		"State":      "completed",
	})

	requireQueryRow(t, actionStateRows, map[string]interface{}{
		"ActionKind": "reveal_frontend_contract",
		"TargetRef":  "load-audit",
		"State":      "skipped",
	})

	requireQueryRow(t, actionStateRows, map[string]interface{}{
		"ActionKind": "navigate_page_route",
		"TargetRef":  "/admin",
		"State":      "pending",
	})

	resumeRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_resume_action("session-resume", "run-resume", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason).`,
	)

	if len(resumeRows) != 1 {
		t.Fatalf("expected exactly one resume action, got %+v", resumeRows)
	}

	requireQueryRow(t, resumeRows, map[string]interface{}{
		"ActionKind":      "navigate_page_route",
		"TargetRef":       "/admin",
		"PageRoute":       "/admin",
		"ApiRoute":        "/api/admin/audit",
		"RequestId":       "",
		"Priority":        int64(70),
		"HazardClass":     "non_mutating_navigation",
		"MutabilityClass": "non_mutating",
		"Reason":          "replay_contract_gap",
	})

	pendingResumeRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_resume_action("session-resume", "run-pending", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason).`,
	)
	if len(pendingResumeRows) != 0 {
		t.Fatalf("expected no resume actions for untouched pending run, got %+v", pendingResumeRows)
	}

	pendingStateRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_state("session-resume", "run-pending", State).`,
	)
	if len(pendingStateRows) != 1 {
		t.Fatalf("expected exactly one pending state row, got %+v", pendingStateRows)
	}
	requireQueryRow(t, pendingStateRows, map[string]interface{}{"State": "pending"})

	resumeStateRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_state("session-resume", "run-resume", State).`,
	)
	if len(resumeStateRows) != 1 {
		t.Fatalf("expected exactly one resume_ready state row, got %+v", resumeStateRows)
	}
	requireQueryRow(t, resumeStateRows, map[string]interface{}{"State": "resume_ready"})

	skippedStateRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_state("session-resume", "run-skipped", State).`,
	)
	if len(skippedStateRows) != 1 {
		t.Fatalf("expected exactly one skipped state row, got %+v", skippedStateRows)
	}
	requireQueryRow(t, skippedStateRows, map[string]interface{}{"State": "skipped"})

	completeStateRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_state("session-resume", "run-complete", State).`,
	)
	if len(completeStateRows) != 1 {
		t.Fatalf("expected exactly one complete state row, got %+v", completeStateRows)
	}
	requireQueryRow(t, completeStateRows, map[string]interface{}{"State": "complete"})

	completeResumeRows := queryAuditResults(
		t,
		engine,
		`scoped_audit_run_resume_action("session-resume", "run-complete", ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason).`,
	)
	if len(completeResumeRows) != 0 {
		t.Fatalf("expected no resume actions for completed run, got %+v", completeResumeRows)
	}
}
