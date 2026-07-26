# BrowserNERD schema module: element reliability, page state, accessibility, and form quality
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 8: ELEMENT FINGERPRINT TRACKING (Reliability Monitoring)
# =============================================================================
# Tracks element stability across page interactions for debugging flaky tests.
# Leverages the new AltSelectors field in ElementFingerprint struct.

# --- Element Fingerprint Facts (pushed by get-interactive-elements) ---
# Captures element identification data for reliable re-finding
Decl element_fingerprint(Ref, TagName, Id, Name, DataTestId, GeneratedAt).

# Alternative selectors for fallback lookup
Decl element_alt_selector(Ref, SelectorIndex, Selector).

# Element lookup outcome tracking (pushed by interact tool)
Decl element_lookup_result(Ref, Strategy, Success, Timestamp).
# Strategy: "testid", "aria", "id", "name", "css_raw", "css_escaped", "alt_selector"

# --- Derived: Unreliable Elements (needed fallback to find) ---
Decl unreliable_element(Ref).
unreliable_element(Ref) :-
    element_lookup_result(Ref, "alt_selector", "true", _).

# --- Derived: Elements that failed all lookups ---
Decl element_not_found(Ref, Timestamp).
element_not_found(Ref, Timestamp) :-
    element_lookup_result(Ref, _, "false", Timestamp).

# =============================================================================
# VECTOR 9: PAGE STATE DETECTION (Common UI Patterns)
# =============================================================================
# Detects common frontend UI states for automated testing assertions.

# --- Base Facts (pushed by page analysis tools) ---
Decl page_state(SessionId, State, Timestamp).
# State: "loading", "loaded", "error", "empty", "authenticating"

Decl loading_indicator_present(SessionId, Count, Timestamp).
Decl empty_state_present(SessionId, Message, Timestamp).
Decl error_boundary_present(SessionId, Message, Timestamp).

# --- Derived: Page is still loading ---
Decl page_loading(SessionId).
page_loading(SessionId) :-
    loading_indicator_present(SessionId, Count, _),
    Count > 0.

# --- Derived: Page shows error state ---
Decl page_has_error(SessionId, Message).
page_has_error(SessionId, Msg) :-
    error_boundary_present(SessionId, Msg, _).

page_has_error(SessionId, Msg) :-
    toast_notification(SessionId, Msg, "error", _, _).

# --- Derived: Page shows empty state ---
Decl page_empty(SessionId, Message).
page_empty(SessionId, Msg) :-
    empty_state_present(SessionId, Msg, _).

# =============================================================================
# VECTOR 10: ACCESSIBILITY AUDIT (A11y Checks)
# =============================================================================
# Rules for detecting common accessibility issues during frontend testing.

# --- Base Facts (pushed by accessibility audit tool) ---
Decl a11y_issue(SessionId, Severity, Rule, Element, Message, Timestamp).
# Severity: "critical", "serious", "moderate", "minor"
# Rule: "missing-alt", "missing-label", "color-contrast", "focus-order", etc.

# --- Derived: Critical accessibility failures ---
Decl a11y_critical(SessionId, Rule, Element, Message).
a11y_critical(SessionId, Rule, Element, Msg) :-
    a11y_issue(SessionId, "critical", Rule, Element, Msg, _).

a11y_critical(SessionId, Rule, Element, Msg) :-
    a11y_issue(SessionId, "serious", Rule, Element, Msg, _).

# --- Interactive element without label (common issue) ---
Decl unlabeled_interactive(SessionId, Ref).
unlabeled_interactive(SessionId, Ref) :-
    interactive(SessionId, Ref, _, "", _).

# --- Form input without accessible name ---
Decl unlabeled_input(SessionId, Ref).
unlabeled_input(SessionId, Ref) :-
    interactive(SessionId, Ref, "input", "", _).

# =============================================================================
# VECTOR 11: FORM VALIDATION DETECTION
# =============================================================================
# Detects form validation states for testing form submissions.

# --- Base Facts (pushed by form analysis) ---
Decl form_validation_error(SessionId, FieldRef, Message, Timestamp).
Decl form_field_invalid(SessionId, FieldRef, Timestamp).
Decl form_field_required(SessionId, FieldRef, IsFilled, Timestamp).

# --- Derived: Form has validation errors ---
Decl form_has_errors(SessionId).
form_has_errors(SessionId) :-
    form_validation_error(SessionId, _, _, _).

# --- Derived: Required field is empty ---
Decl missing_required_field(SessionId, FieldRef).
missing_required_field(SessionId, FieldRef) :-
    form_field_required(SessionId, FieldRef, "false", _).

# --- Derived: Form ready to submit (no errors, all required filled) ---
# Note: This is a conceptual rule - actual implementation needs negation handling in Go

# =============================================================================
# VECTOR 12: INTERACTION SEQUENCE TRACKING
# =============================================================================
# Tracks sequences of user interactions for reproducing test scenarios.

# --- Derived: Actions taken on same element ---
Decl repeated_action_on_element(SessionId, Ref, ActionCount).
repeated_action_on_element(SessionId, Ref, Count) :-
    user_click(SessionId, Ref, _) |>
    do fn:group_by(SessionId, Ref),
    let Count = fn:count().

# --- Derived: Click followed by type (common form pattern) ---
Decl click_then_type(SessionId, ClickRef, TypeRef, TimeDelta).
click_then_type(SessionId, ClickRef, TypeRef, Delta) :-
    user_click(SessionId, ClickRef, TClick),
    user_type(SessionId, TypeRef, _, TType),
    TType > TClick,
    Delta = fn:minus(TType, TClick),
    Delta < 5000.

# --- Derived: Navigation after button click ---
Decl click_triggered_navigation(SessionId, Ref, FromUrl, ToUrl, TimeDelta).
click_triggered_navigation(SessionId, Ref, FromUrl, ToUrl, Delta) :-
    user_click(SessionId, Ref, TClick),
    navigation_event(SessionId, ToUrl, TNav),
    TNav > TClick,
    fn:minus(TNav, TClick) < 5000,
    current_url(SessionId, FromUrl),
    FromUrl != ToUrl,
    Delta = fn:minus(TNav, TClick).

# =============================================================================
# VECTOR 13: TEST ASSERTION HELPERS
# =============================================================================
# Common assertion patterns for frontend testing.

# --- Element exists with its label text ---
Decl element_has_text(SessionId, Ref, Label).
element_has_text(SessionId, Ref, Label) :-
    interactive(SessionId, Ref, _, Label, _).

# --- Element is in expected state ---
Decl element_is_enabled(SessionId, Ref).
element_is_enabled(SessionId, Ref) :-
    element_enabled(SessionId, Ref, "true").

Decl element_is_disabled(SessionId, Ref).
element_is_disabled(SessionId, Ref) :-
    element_enabled(SessionId, Ref, "false").

# --- Page current URL (alias for queries) ---
Decl at_route(SessionId, Url).
at_route(SessionId, Url) :-
    current_url(SessionId, Url).

# --- No console errors on page ---
Decl page_has_console_errors(SessionId).
page_has_console_errors(SessionId) :-
    console_event(SessionId, "error", _, _),
    current_url(SessionId, _).

# --- API request succeeded (any 2xx response) ---
Decl api_success(SessionId, Url).
api_success(SessionId, Url) :-
    net_request(SessionId, ReqId, _, Url, _, _),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 200,
    Status < 300.
