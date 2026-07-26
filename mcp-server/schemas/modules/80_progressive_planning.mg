# BrowserNERD schema module: progressive disclosure, gates, and action planning
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 15: PROGRESSIVE DISCLOSURE + JS GATING
# =============================================================================
# Supports token-efficient tool responses and controlled escalation to JS.

# Evidence handles emitted by consolidated tools.
Decl disclosure_handle(SessionId, Handle, Reason, Timestamp).

# Gate facts that authorize evaluate-js fallback.
Decl js_gate_open(SessionId, Reason, Timestamp).

# Confidence emitted by browser-reason for topic-level trust scoring.
Decl confidence_score(SessionId, Topic, Score, Timestamp).

# Derived low-confidence marker.
Decl low_confidence_topic(SessionId, Topic).
low_confidence_topic(SessionId, Topic) :-
    confidence_score(SessionId, Topic, Score, _),
    Score < 70.

# Derived disclosure escalation signal.
Decl disclosure_escalation(SessionId, Topic, Reason).
disclosure_escalation(SessionId, Topic, "low_confidence") :-
    low_confidence_topic(SessionId, Topic).

# =============================================================================
# VECTOR 16: ACTION PLANNING CANDIDATES (MANGLE-NATIVE)
# =============================================================================
# Lets browser-reason produce browser-act operation plans with fewer tool calls.

Decl action_candidate(SessionId, Ref, Label, Action, Priority, Reason).

action_candidate(SessionId, Ref, Label, "click", 100, "primary_action") :-
    primary_action(SessionId, Ref, Label).

action_candidate(SessionId, Ref, Label, "click", 80, "enabled_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    element_enabled(SessionId, Ref, "true").

action_candidate(SessionId, Ref, Label, "type", 78, "enabled_input") :-
    interactive(SessionId, Ref, "input", Label, "type"),
    element_enabled(SessionId, Ref, "true").

action_candidate(SessionId, Ref, Label, "select", 72, "enabled_select") :-
    interactive(SessionId, Ref, "select", Label, "select"),
    element_enabled(SessionId, Ref, "true").

action_candidate(SessionId, Ref, Label, "toggle", 68, "toggle_control") :-
    interactive(SessionId, Ref, "checkbox", Label, "toggle").

action_candidate(SessionId, Ref, Label, "toggle", 66, "radio_control") :-
    interactive(SessionId, Ref, "radio", Label, "toggle").

action_candidate(SessionId, Ref, Label, "click", 70, "button_click") :-
    interactive(SessionId, Ref, "button", Label, "click").

action_candidate(SessionId, Ref, Label, "click", 60, "link_click") :-
    interactive(SessionId, Ref, "link", Label, "click").

action_candidate(SessionId, Ref, Href, "navigate", 58, "internal_nav_link") :-
    nav_link(SessionId, Ref, Href, _, "true").

action_candidate(SessionId, Ref, Label, "click", 57, "close_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "Close".

action_candidate(SessionId, Ref, Label, "click", 57, "close_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "close".

action_candidate(SessionId, Ref, Label, "click", 56, "dismiss_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "Dismiss".

action_candidate(SessionId, Ref, Label, "click", 56, "dismiss_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "dismiss".

action_candidate(SessionId, Ref, Label, "click", 55, "cancel_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "Cancel".

action_candidate(SessionId, Ref, Label, "click", 55, "cancel_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "cancel".

action_candidate(SessionId, Ref, Label, "click", 54, "retry_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "Retry".

action_candidate(SessionId, Ref, Label, "click", 54, "retry_button") :-
    interactive(SessionId, Ref, "button", Label, "click"),
    Label = "retry".

Decl global_action(SessionId, Action, Priority, Reason).
global_action(SessionId, "press_escape", 110, Reason) :-
    interaction_blocked(SessionId, Reason).
