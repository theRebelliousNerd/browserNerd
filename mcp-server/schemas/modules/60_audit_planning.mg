# BrowserNERD schema module: audit state, hazards, and deterministic planning
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 13c: AUDIT PLAN + HAZARD STATE
# =============================================================================
# Progressive audit discovery persists deterministic plan state in Mangle so
# agents can query/inspect hazards without expanding raw plan JSON.

Decl audit_plan_state(SessionId, AuditId, Phase, Status, Timestamp).
Decl audit_plan_step(SessionId, AuditId, Step, StepId, Tool, Risky).
Decl audit_plan_step_completed(SessionId, AuditId, StepId, Timestamp).
Decl audit_plan_step_skipped(SessionId, AuditId, StepId, Reason, Timestamp).
Decl audit_discovered_action(SessionId, AuditId, Step, Ref, Action, Label, Route).
Decl audit_hazard_fact(SessionId, AuditId, Step, Hazard, Severity).

Decl audit_plan_step_pending(SessionId, AuditId, Step, StepId, Tool, Risky).
audit_plan_step_pending(SessionId, AuditId, Step, StepId, Tool, Risky) :-
    audit_plan_step(SessionId, AuditId, Step, StepId, Tool, Risky),
    !audit_plan_step_completed(SessionId, AuditId, StepId, _),
    !audit_plan_step_skipped(SessionId, AuditId, StepId, _, _).

Decl audit_navigation_action(SessionId, AuditId, Step).
audit_navigation_action(SessionId, AuditId, Step) :-
    audit_discovered_action(SessionId, AuditId, Step, Ref, "navigate", Label, Route).

Decl audit_write_action(SessionId, AuditId, Step).
audit_write_action(SessionId, AuditId, Step) :-
    audit_discovered_action(SessionId, AuditId, Step, Ref, "submit", Label, Route).

audit_write_action(SessionId, AuditId, Step) :-
    audit_hazard_fact(SessionId, AuditId, Step, "write_action", Severity).

audit_write_action(SessionId, AuditId, Step) :-
    audit_hazard_fact(SessionId, AuditId, Step, "write_form", Severity).

audit_write_action(SessionId, AuditId, Step) :-
    audit_hazard_fact(SessionId, AuditId, Step, "destructive_action", Severity).

Decl audit_requires_approval(SessionId, AuditId).
audit_requires_approval(SessionId, AuditId) :-
    audit_hazard_fact(SessionId, AuditId, Step, Hazard, "high").

audit_requires_approval(SessionId, AuditId) :-
    audit_hazard_fact(SessionId, AuditId, Step, Hazard, "medium").

# =============================================================================
# VECTOR 13d: DETERMINISTIC AUDIT PLANNING
# =============================================================================
# Produces deterministic, session-scoped follow-up actions from the scoped
# contract findings above. The planner only auto-emits non-mutating reveal /
# navigation steps; write or destructive steps must be surfaced explicitly as
# proposed actions so tooling can gate them before execution.
#
# Performance note:
# Every rule anchors on a derived scoped finding (or a single audit run record)
# before helper predicates or negation are applied, which keeps joins narrow and
# avoids cross-session plan fan-out.

Decl scoped_audit_plan_action(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

scoped_audit_plan_action(SessionId, "reveal_request_auth", RequestId, PageRoute, ApiRoute, RequestId, 100, "missing_auth") :-
    scoped_missing_jwt_or_auth_header(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ExpectedMechanism).

scoped_audit_plan_action(SessionId, "reveal_request_api_key", RequestId, PageRoute, ApiRoute, RequestId, 99, "missing_api_key") :-
    scoped_missing_api_key(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method).

scoped_audit_plan_action(SessionId, "reveal_request_auth", RequestId, PageRoute, ApiRoute, RequestId, 98, "auth_mismatch") :-
    scoped_auth_mechanism_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ObservedMechanism, ExpectedMechanism).

scoped_audit_plan_action(SessionId, "reveal_request_payload", Field, PageRoute, ApiRoute, RequestId, 96, "payload_requirement_mismatch") :-
    scoped_payload_requirement_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, Field, Requirement).

scoped_audit_plan_action(SessionId, "reveal_frontend_contract", ActionRef, PageRoute, ApiRoute, "", 94, "backend_contract_gap") :-
    scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, "backend_contract", "declared", "missing").

scoped_audit_plan_action(SessionId, "reveal_contract_diff", Aspect, PageRoute, ApiRoute, "", 92, "contract_gap") :-
    scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Aspect, FrontendValue, BackendValue),
    Aspect != "backend_contract".

scoped_audit_plan_action(SessionId, "navigate_page_route", PageRoute, PageRoute, ApiRoute, "", 70, "replay_contract_gap") :-
    scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Aspect, FrontendValue, BackendValue).

Decl scoped_audit_plan_hazard(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, MutabilityClass, HazardClass, Severity, HazardReason).

scoped_audit_plan_hazard(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "non_mutating", "non_mutating_reveal", "low", "reveal_existing_evidence") :-
    scoped_audit_plan_action(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason),
    :string:contains(ActionKind, "reveal_").

scoped_audit_plan_hazard(SessionId, "navigate_page_route", TargetRef, PageRoute, ApiRoute, RequestId, "non_mutating", "non_mutating_navigation", "medium", "replay_route_without_form_mutation") :-
    scoped_audit_plan_action(SessionId, "navigate_page_route", TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

# Proposed actions let tooling classify user-requested or tool-requested steps
# before execution. This is where write/destructive hazards are surfaced.
Decl scoped_audit_proposed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Reason).
Decl scoped_audit_proposed_action_hazard(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, MutabilityClass, HazardClass, Severity, HazardReason).

scoped_audit_proposed_action_hazard(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "non_mutating", "non_mutating_reveal", "low", "reveal_existing_evidence") :-
    scoped_audit_proposed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Reason),
    :string:contains(ActionKind, "reveal_").

scoped_audit_proposed_action_hazard(SessionId, RunId, "navigate_page_route", TargetRef, PageRoute, ApiRoute, RequestId, "non_mutating", "non_mutating_navigation", "medium", "replay_route_without_form_mutation") :-
    scoped_audit_proposed_action(SessionId, RunId, "navigate_page_route", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "navigate_history", TargetRef, PageRoute, ApiRoute, RequestId, "non_mutating", "non_mutating_navigation", "medium", "history_navigation_replay") :-
    scoped_audit_proposed_action(SessionId, RunId, "navigate_history", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "click", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "click_can_mutate_page_state") :-
    scoped_audit_proposed_action(SessionId, RunId, "click", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "type", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "typing_mutates_form_state") :-
    scoped_audit_proposed_action(SessionId, RunId, "type", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "select", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "selection_mutates_form_state") :-
    scoped_audit_proposed_action(SessionId, RunId, "select", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "toggle", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "toggle_mutates_form_state") :-
    scoped_audit_proposed_action(SessionId, RunId, "toggle", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "submit", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "submit_can_trigger_persistent_side_effects") :-
    scoped_audit_proposed_action(SessionId, RunId, "submit", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "write_hazard", "high", "save_like_action_can_write_state") :-
    scoped_audit_proposed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Reason),
    :string:contains(ActionKind, "save").

scoped_audit_proposed_action_hazard(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "destructive_hazard", "critical", "destructive_side_effect") :-
    scoped_audit_proposed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Reason),
    :string:contains(ActionKind, "delete").

scoped_audit_proposed_action_hazard(SessionId, RunId, "remove", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "destructive_hazard", "critical", "destructive_side_effect") :-
    scoped_audit_proposed_action(SessionId, RunId, "remove", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "destroy", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "destructive_hazard", "critical", "destructive_side_effect") :-
    scoped_audit_proposed_action(SessionId, RunId, "destroy", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

scoped_audit_proposed_action_hazard(SessionId, RunId, "purge", TargetRef, PageRoute, ApiRoute, RequestId, "mutating", "destructive_hazard", "critical", "destructive_side_effect") :-
    scoped_audit_proposed_action(SessionId, RunId, "purge", TargetRef, PageRoute, ApiRoute, RequestId, Reason).

# Resumable run state lets tooling checkpoint and later resume the remaining
# deterministic audit actions without recomputing imperative state.
Decl scoped_audit_run(SessionId, RunId, Focus, StartedAt).
Decl scoped_audit_run_completed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, CompletedAt).
Decl scoped_audit_run_skipped_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, SkipReason, Timestamp).

Decl scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).
scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason) :-
    scoped_audit_run(SessionId, RunId, Focus, StartedAt),
    scoped_audit_plan_action(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

Decl scoped_audit_run_has_action(SessionId, RunId).
scoped_audit_run_has_action(SessionId, RunId) :-
    scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

Decl scoped_audit_run_completed_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).
scoped_audit_run_completed_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId) :-
    scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason),
    scoped_audit_run_completed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, CompletedAt).

Decl scoped_audit_run_has_completed_action(SessionId, RunId).
scoped_audit_run_has_completed_action(SessionId, RunId) :-
    scoped_audit_run_completed_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).

Decl scoped_audit_run_skipped_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).
scoped_audit_run_skipped_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId) :-
    scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason),
    scoped_audit_run_skipped_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, SkipReason, Timestamp).

Decl scoped_audit_run_has_skipped_action(SessionId, RunId).
scoped_audit_run_has_skipped_action(SessionId, RunId) :-
    scoped_audit_run_skipped_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).

Decl scoped_audit_run_has_handled_action(SessionId, RunId).
scoped_audit_run_has_handled_action(SessionId, RunId) :-
    scoped_audit_run_has_completed_action(SessionId, RunId).

scoped_audit_run_has_handled_action(SessionId, RunId) :-
    scoped_audit_run_has_skipped_action(SessionId, RunId).

Decl scoped_audit_run_pending_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).
scoped_audit_run_pending_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason) :-
    scoped_audit_run_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason),
    !scoped_audit_run_completed_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId),
    !scoped_audit_run_skipped_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).

Decl scoped_audit_run_action_state(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, State).
scoped_audit_run_action_state(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "completed") :-
    scoped_audit_run_completed_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).

scoped_audit_run_action_state(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "skipped") :-
    scoped_audit_run_skipped_exists(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId).

scoped_audit_run_action_state(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, "pending") :-
    scoped_audit_run_pending_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

Decl scoped_audit_run_resume_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason).
scoped_audit_run_resume_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason) :-
    scoped_audit_run_pending_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason),
    scoped_audit_run_has_handled_action(SessionId, RunId),
    scoped_audit_plan_hazard(SessionId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, MutabilityClass, HazardClass, Severity, HazardReason).

Decl scoped_audit_run_has_pending_action(SessionId, RunId).
scoped_audit_run_has_pending_action(SessionId, RunId) :-
    scoped_audit_run_pending_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, Reason).

Decl scoped_audit_run_state(SessionId, RunId, State).
scoped_audit_run_state(SessionId, RunId, "pending") :-
    scoped_audit_run_has_pending_action(SessionId, RunId),
    !scoped_audit_run_has_handled_action(SessionId, RunId).

scoped_audit_run_state(SessionId, RunId, "resume_ready") :-
    scoped_audit_run_has_pending_action(SessionId, RunId),
    scoped_audit_run_has_handled_action(SessionId, RunId).

scoped_audit_run_state(SessionId, RunId, "skipped") :-
    scoped_audit_run_has_action(SessionId, RunId),
    !scoped_audit_run_has_pending_action(SessionId, RunId),
    !scoped_audit_run_has_completed_action(SessionId, RunId),
    scoped_audit_run_has_skipped_action(SessionId, RunId).

scoped_audit_run_state(SessionId, RunId, "complete") :-
    scoped_audit_run(SessionId, RunId, Focus, StartedAt),
    !scoped_audit_run_has_pending_action(SessionId, RunId),
    !scoped_audit_run_has_action(SessionId, RunId).

scoped_audit_run_state(SessionId, RunId, "complete") :-
    scoped_audit_run_has_completed_action(SessionId, RunId),
    !scoped_audit_run_has_pending_action(SessionId, RunId).
