# BrowserNERD MCP Schema (Mangle)
# Implements the four PRD vectors:
#   1. Semantic DOM/state reification (React Fiber)
#   2. Flight Recorder for debugging (CDP event stream)
#   3. Session persistence (detached browser)
#   4. Logic-based test assertions

# =============================================================================
# VECTOR 1: REACT FIBER REIFICATION (Developer Context)
# =============================================================================

# Component tree extracted from __reactFiber keys
# Session-scoped to prevent cross-session contamination.
Decl react_component(SessionId, FiberId, ComponentName, ParentFiberId).
Decl react_prop(SessionId, FiberId, PropKey, PropValue).
Decl react_state(SessionId, FiberId, HookIndex, Value).
Decl dom_mapping(SessionId, FiberId, DomNodeId).

# =============================================================================
# VECTOR 2: FLIGHT RECORDER (CDP Event Stream)
# =============================================================================

# --- DOM Structure (sampled snapshots) ---
# Session-scoped to prevent cross-session contamination.
Decl dom_node(SessionId, NodeId, Tag, Text, ParentId).
Decl dom_attr(SessionId, NodeId, Key, Value).
Decl dom_text(SessionId, NodeId, Text).
Decl dom_updated(SessionId, Timestamp).
Decl dom_layout(SessionId, NodeId, X, Y, Width, Height, Visible).

# --- Page Stability (for await-stable-state) ---
Decl page_stable().
# A page is stable if:
# 1. No network requests in last 500ms
# 2. No DOM updates in last 200ms
# (Note: This is a conceptual rule; the tool will implement the logic using temporal queries)


# --- Network Events (HAR-like schema per PRD Section 3.3) ---
# Core transaction record
# Session-scoped to prevent cross-session contamination.
Decl net_request(SessionId, Id, Method, Url, InitiatorId, StartTime).
# Response metadata with timing
Decl net_response(SessionId, Id, Status, Latency, Duration).
# Normalized headers (keys lowercased)
Decl net_header(SessionId, Id, Kind, Key, Value).
# Correlation keys normalized from headers (request_id/correlation_id/trace_id)
Decl net_correlation_key(SessionId, Id, KeyType, KeyValue).
# Critical for causality: what triggered this request?
Decl request_initiator(SessionId, Id, Type, ScriptId).

# --- Browser/User Events ---
# Session-scoped to prevent cross-session contamination.
Decl console_event(SessionId, Level, Message, Timestamp).
Decl click_event(SessionId, NodeId, Timestamp).
Decl input_event(SessionId, NodeId, Value, Timestamp).
Decl state_change(SessionId, Name, Value, Timestamp).
Decl navigation_event(SessionId, Url, Timestamp).

# --- Session State (for current_url predicate) ---
Decl current_url(SessionId, Url).

# --- Toast/Notification Events (instant error overlay detection) ---
# Captured via MutationObserver watching for toast elements in real-time
# Session-scoped to prevent cross-session contamination.
Decl toast_notification(SessionId, Text, Level, Source, Timestamp).
# Level: "error", "warning", "success", "info"
# Source: UI library (material-ui, chakra-ui, ant-design, shadcn, react-toastify, react-hot-toast, notistack, native)

# Convenience predicates for level-specific queries
Decl error_toast(SessionId, Text, Source, Timestamp).
Decl warning_toast(SessionId, Text, Source, Timestamp).

# =============================================================================
# DERIVED FACTS (Causal Reasoning / RCA)
# =============================================================================

Decl caused_by(SessionId, ConsoleMessage, RequestId).
Decl slow_api(SessionId, RequestId, Url, Duration).
Decl cascading_failure(SessionId, ChildReqId, ParentReqId).
Decl race_condition_detected(SessionId).
Decl test_passed(SessionId).
Decl failed_request(SessionId, RequestId, Url, Status).
Decl failed_request_at(SessionId, RequestId, Url, Status, RequestTimestamp).
Decl slow_api_at(SessionId, RequestId, Url, Duration, RequestTimestamp).
Decl error_chain(SessionId, ConsoleErr, RequestId, Url, Status).
Decl root_cause_at(SessionId, ConsoleMsg, Source, Cause, Timestamp).

# Toast correlation predicates
Decl toast_after_api_failure(SessionId, ToastText, RequestId, Url, Status, TimeDelta).
Decl user_visible_error(SessionId, Source, Message, Timestamp).
Decl repeated_toast_error(SessionId, Message).
Decl toast_error_chain(SessionId, ToastText, RequestId, Url, Status).

# =============================================================================
# CAUSAL REASONING RULES (PRD Section 3.4)
# =============================================================================

# Rule 1: API-Triggered Crash Detection
# A console error is caused by a request if:
#   1. The request failed (Status >= 400)
#   2. The request finished BEFORE the error appeared
#   3. The time difference is less than 100ms (temporal proximity)
caused_by(SessionId, ConsoleErr, ReqId) :-
    console_event(SessionId, "error", ConsoleErr, TError),
    failed_request_at(SessionId, ReqId, _, _, TNet),
    TNet < TError,
    fn:minus(TError, TNet) < 100.

# Rule 2: Slow API Detection (>1 second duration)
# Flags API calls exceeding performance SLA
slow_api(SessionId, ReqId, Url, Duration) :-
    net_request(SessionId, ReqId, _, Url, _, _),
    net_response(SessionId, ReqId, _, _, Duration),
    Duration > 1000.

# Rule 3: Cascading Failure Detection
# A child request fails because its parent (initiator) also failed
cascading_failure(SessionId, ChildReqId, ParentReqId) :-
    request_initiator(SessionId, ChildReqId, _, ParentReqId),
    failed_request(SessionId, ChildReqId, _, _),
    failed_request(SessionId, ParentReqId, _, _).

# Rule 4: Race Condition Detection (PRD Section 5.3)
# Detects when a submit button is clicked before the form state is ready
Decl submit_button_clicked(SessionId, BtnId, Timestamp).
submit_button_clicked(SessionId, BtnId, TimeClick) :-
    click_event(SessionId, BtnId, TimeClick),
    dom_attr(SessionId, BtnId, "id", "submit-btn").

race_condition_detected(SessionId) :-
    submit_button_clicked(SessionId, _, TimeClick),
    state_change(SessionId, "isReady", "true", TimeReady),
    TimeClick < TimeReady.

# Rule 5: Failed Request Summary
# Convenience predicate for listing all failed requests
failed_request(SessionId, ReqId, Url, Status) :-
    net_request(SessionId, ReqId, _, Url, _, _),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 400.

failed_request_at(SessionId, ReqId, Url, Status, ReqTs) :-
    net_request(SessionId, ReqId, _, Url, _, ReqTs),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 400.

# Rule 6: Full Error Chain
# Links console errors to their causal network requests with full context
error_chain(SessionId, ConsoleErr, ReqId, Url, Status) :-
    caused_by(SessionId, ConsoleErr, ReqId),
    net_request(SessionId, ReqId, _, Url, _, _),
    net_response(SessionId, ReqId, Status, _, _).

# =============================================================================
# TOAST/NOTIFICATION CORRELATION RULES (Instant Error Detection)
# =============================================================================
# These rules enable immediate detection of user-visible errors via toast overlays,
# which appear before console errors and provide better UX correlation.

# Rule 7: Toast Appeared After API Failure
# Correlates error toasts with failed API requests within 5 seconds
# This detects when the UI shows an error message due to a backend failure
toast_after_api_failure(SessionId, ToastText, ReqId, Url, Status, TimeDelta) :-
    error_toast(SessionId, ToastText, _, TToast),
    failed_request_at(SessionId, ReqId, Url, Status, TReq),
    TToast > TReq,
    TimeDelta = fn:minus(TToast, TReq),
    TimeDelta < 5000.

# Rule 8: User Visible Errors (unified view)
# Aggregates all user-visible errors from different sources
user_visible_error(SessionId, "toast", Msg, Ts) :-
    error_toast(SessionId, Msg, _, Ts).

user_visible_error(SessionId, "console", Msg, Ts) :-
    console_event(SessionId, "error", Msg, Ts).

# Rule 9: Repeated Toast Errors
# Detects when the same error toast appears multiple times (systemic issue)
repeated_toast_error(SessionId, Msg) :-
    error_toast(SessionId, Msg, _, T1),
    error_toast(SessionId, Msg, _, T2),
    T1 != T2.

# Rule 10: Toast Error Chain
# Full chain: Error toast -> Failed API -> URL and status
# Similar to error_chain but for toast-based detection
toast_error_chain(SessionId, ToastText, ReqId, Url, Status) :-
    toast_after_api_failure(SessionId, ToastText, ReqId, Url, Status, _).

# Rule 11: Toast Without API Correlation
# Detects error toasts that don't correlate with any API failure
# (could indicate client-side validation errors or other issues)
# Note: Requires tracking in Go code since Mangle negation is limited

# =============================================================================
# VECTOR 4: LOGIC-BASED TEST ASSERTIONS (PRD Section 5)
# =============================================================================

# Generic test_passed rule: navigated to dashboard AND welcome message visible
# Agents can submit custom rules via submit-rule tool
test_passed(SessionId) :-
    current_url(SessionId, "/dashboard"),
    dom_text(SessionId, _, "Welcome User").

# Alternative: Check navigation_event if current_url not maintained
# test_passed(SessionId) :-
#     navigation_event(_, Url, _),
#     fn:string_contains(Url, "/dashboard"),
#     dom_text(SessionId, _, "Welcome").

# =============================================================================
# VECTOR 5: INTERACTIVE ELEMENT NAVIGATION (Token-Efficient)
# =============================================================================

# Interactive elements extracted by get-interactive-elements tool
# Ref is the element identifier (id, name, or selector)
Decl interactive(SessionId, Ref, Type, Label, Action).

# Element state for diagnostic purposes
Decl element_visible(SessionId, Ref, Visible).
Decl element_enabled(SessionId, Ref, Enabled).
Decl element_value(SessionId, Ref, Value).
Decl elem_attr(SessionId, Ref, AttrName, AttrValue).
Decl elem_class(SessionId, Ref, Class).
Decl elem_bbox(SessionId, Ref, X, Y, Width, Height).

# User interaction events (emitted by interact tool)
Decl user_click(SessionId, Ref, Timestamp).
Decl user_type(SessionId, Ref, Value, Timestamp).
Decl user_select(SessionId, Ref, Option, Timestamp).
Decl user_toggle(SessionId, Ref, Timestamp).

# =============================================================================
# VECTOR 5b: HYPER-EFFICIENT NAVIGATION (get-navigation-links tool)
# =============================================================================

# Navigation links extracted by get-navigation-links tool
# Area is one of: "nav", "side", "main", "foot"
# Internal is "true" or "false" string
Decl nav_link(SessionId, Ref, Href, Area, Internal).

# Derived: Count links by area
Decl nav_area_has_links(SessionId, Area).
nav_area_has_links(SessionId, Area) :- nav_link(SessionId, _, _, Area, _).

# Derived: Find internal navigation targets
Decl internal_nav_target(SessionId, Href).
internal_nav_target(SessionId, Href) :- nav_link(SessionId, _, Href, _, "true").

# Derived: Find external links (potential security/UX concern)
Decl external_link(SessionId, Ref, Href, Area).
external_link(SessionId, Ref, Href, Area) :- nav_link(SessionId, Ref, Href, Area, "false").

# =============================================================================
# INTERACTION DIAGNOSTIC RULES
# =============================================================================

# Rule: Click on non-visible element (potential failure)
# Note: element_visible stores "true" or "false" as strings
Decl click_on_hidden(SessionId, Ref).
click_on_hidden(SessionId, Ref) :-
    user_click(SessionId, Ref, _),
    element_visible(SessionId, Ref, "false").

# Rule: Click on disabled element
Decl click_on_disabled(SessionId, Ref).
click_on_disabled(SessionId, Ref) :-
    user_click(SessionId, Ref, _),
    element_enabled(SessionId, Ref, "false").

# Diagnostic predicates for code-level tracking
# (Mangle's negation semantics differ from Prolog - track these via code)
Decl invalid_type_target(Ref).
Decl undiscovered_interaction(Ref).

# =============================================================================
# VECTOR 6: ADVANCED AUTOMATION EVENTS
# =============================================================================

# Screenshot events
Decl screenshot_taken(SessionId, Format, SizeBytes, Timestamp).

# Browser history navigation
Decl history_navigation(SessionId, Action, Url, Timestamp).

# JavaScript evaluation
Decl js_evaluated(SessionId, ScriptLength, Timestamp).

# Form automation
Decl form_field_filled(SessionId, Ref, Timestamp).
Decl form_submitted(SessionId, FieldCount, Timestamp).

# Keypress events
Decl user_keypress(SessionId, Key, Timestamp).

# Plan execution
Decl plan_executed(SessionId, TotalActions, Succeeded, Failed, Timestamp).

# Action queue (for execute-plan tool)
# Claude submits these via submit-rule, execute-plan reads and executes them
Decl action(ActionType, Ref, Value).

# =============================================================================
# MANGLE-DRIVEN AUTOMATION RULES
# =============================================================================

# Rule: Login form detected (common pattern)
Decl login_form_detected(SessionId).
Decl email_input(SessionId, Ref).
email_input(SessionId, Ref) :-
    interactive(SessionId, Ref, "input", _, _),
    elem_attr(SessionId, Ref, "input_type", "email").

Decl password_input(SessionId, Ref).
password_input(SessionId, Ref) :-
    interactive(SessionId, Ref, "input", _, _),
    elem_attr(SessionId, Ref, "input_type", "password").

login_form_detected(SessionId) :-
    current_url(SessionId, _),
    email_input(SessionId, _),
    password_input(SessionId, _).

# Rule: Form ready for submission
Decl form_ready(SessionId).
form_ready(SessionId) :-
    form_field_filled(SessionId, _, T1),
    form_field_filled(SessionId, _, T2),
    T1 != T2.

# =============================================================================
# UNIVERSAL LOGIN SUCCESS DETECTION
# =============================================================================
# A comprehensive, site-agnostic approach to detecting successful logins.
# Works by tracking URL state before/after form submission and analyzing
# the navigation pattern combined with API response success.

# --- Pre-submit URL tracking ---
# The Go code should emit this fact when form_submitted is about to fire,
# capturing the URL the user was on before submitting (typically a login page).
Decl url_before_submit(SessionId, Url, Timestamp).

# --- Successful API response tracking ---
# Track successful POST requests (common for login flows)
Decl successful_post(SessionId, RequestId, Url, Timestamp).
successful_post(SessionId, ReqId, Url, TReq) :-
    net_request(SessionId, ReqId, "POST", Url, _, TReq),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 200,
    Status < 300.

# --- Navigation change detection ---
# Detects when URL changed after form submission (universal pattern)
Decl url_changed_after_submit(SessionId, UrlBefore, UrlAfter, TNav).
url_changed_after_submit(SessionId, UrlBefore, UrlAfter, TNav) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, UrlBefore, TBefore),
    TSubmit > TBefore,
    navigation_event(SessionId, UrlAfter, TNav),
    TNav > TSubmit,
    UrlBefore != UrlAfter.

# --- Primary login success rule ---
# Login succeeded when:
#   1. URL changed after form submission (universal - works on any site)
#   2. A successful POST occurred around the same time (confirms backend accepted)
#   3. Navigation happened within 5 seconds of submit (reasonable timeout)
Decl login_succeeded(SessionId).
login_succeeded(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    successful_post(SessionId, _, _, TPost),
    TPost >= TSubmit,
    url_changed_after_submit(SessionId, _, _, TNav),
    fn:minus(TNav, TSubmit) < 5000.

# --- Alternative: Navigation-only success (no POST required) ---
# Some sites use client-side routing without a POST (OAuth redirects, etc.)
# This fires if URL changes after submit, even without a successful POST
Decl login_succeeded_navigation_only(SessionId).
login_succeeded_navigation_only(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    url_changed_after_submit(SessionId, _, _, TNav),
    fn:minus(TNav, TSubmit) < 5000.

# --- Login failure detection ---
# Detects when form was submitted but URL didn't change (stayed on login page)
# or when there was a failed API response
Decl login_failed_no_navigation(SessionId).
login_failed_no_navigation(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, UrlBefore, TBefore),
    TSubmit > TBefore,
    current_url(SessionId, UrlBefore).

Decl login_failed_api_error(SessionId, ReqId, Status).
login_failed_api_error(SessionId, ReqId, Status) :-
    form_submitted(SessionId, _, TSubmit),
    net_request(SessionId, ReqId, "POST", _, _, TReq),
    TReq >= TSubmit,
    net_response(SessionId, ReqId, Status, _, _),
    fn:minus(TReq, TSubmit) < 2000,
    Status >= 400.

# --- Session state tracking for login context ---
# Tracks that we're in a "login attempt" state (form submitted, awaiting result)
Decl login_attempt_pending(SessionId, Timestamp).
login_attempt_pending(SessionId, TSubmit) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, _, TBefore),
    TSubmit > TBefore.

# --- Authenticated session indicator ---
# Generic indicator that can be filled by code-level detection
# (e.g., detecting auth cookies, JWT tokens, or session storage)
Decl authenticated_session(SessionId, Method, Timestamp).

# Rule: Failed API call during automation
Decl automation_error(SessionId, ReqId, Url).
automation_error(SessionId, ReqId, Url) :-
    plan_executed(SessionId, _, _, Failed, TPlan),
    Failed > 0,
    net_request(SessionId, ReqId, _, Url, _, TReq),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 400,
    TReq > TPlan.

# =============================================================================
# TOKEN-EFFICIENT TEMPLATES (Claude can submit these patterns)
# =============================================================================

# Template: Fill and submit login form
# Usage: Submit facts like:
#   action("type", "email-input", "user@example.com").
#   action("type", "password-input", "secret123").
#   action("click", "submit-btn").
# Then call execute-plan to run them all.

# Template: Navigate and wait for element
# Usage: Submit rule:
#   action("navigate", "http://example.com/dashboard").
#   ready() :- interactive("dashboard-header", _, _, _).
# Then call execute-plan, then wait-for-condition with predicate="ready".

# =============================================================================
# VECTOR 7: DOCKER LOG INTEGRATION (Full-Stack Error Correlation)
# =============================================================================
# Enables correlation of browser errors with backend container logs for
# comprehensive root cause analysis across the entire stack.

# --- Docker Container Logs (Base Facts) ---
# Pushed by get-console-errors when Docker integration is enabled
# Container name matches docker.containers config (default: "backend", "frontend").
# Level: ERROR, WARNING, INFO, DEBUG, CRITICAL
# Tag: Optional tag like [STARTUP], [AUDIT], [LIFESPAN], [TRACEBACK], [NEXTJS]
Decl docker_log(Container, Level, Tag, Message, Timestamp).
# Parsed correlation keys extracted from Docker log messages.
Decl docker_log_correlation(Container, KeyType, KeyValue, Message, Timestamp).

# --- Derived: Error-level logs by container ---
Decl docker_error(Container, Message, Timestamp).
docker_error(Container, Msg, Ts) :-
    docker_log(Container, "ERROR", _, Msg, Ts).

docker_error(Container, Msg, Ts) :-
    docker_log(Container, "CRITICAL", _, Msg, Ts).

Decl docker_warning(Container, Message, Timestamp).
docker_warning(Container, Msg, Ts) :-
    docker_log(Container, "WARNING", _, Msg, Ts).

# --- Derived: Errors by specific container ---
Decl backend_error(Message, Timestamp).
backend_error(Msg, Ts) :-
    docker_error("backend", Msg, Ts).

# Frontend SSR errors are emitted via Docker logs and are global by default. When correlation keys
# (request_id / correlation_id / trace_id) are present, we can map them back to browser sessions
# using net_correlation_key to avoid cross-session cartesian products.
Decl frontend_ssr_error_global(Message, Timestamp).
frontend_ssr_error_global(Msg, Ts) :-
    docker_error("frontend", Msg, Ts).

Decl frontend_ssr_error_with_key(Message, Timestamp, KeyType, KeyValue).
frontend_ssr_error_with_key(Msg, Ts, KeyType, KeyValue) :-
    frontend_ssr_error_global(Msg, Ts),
    docker_log_correlation("frontend", KeyType, KeyValue, Msg, Ts).

Decl frontend_ssr_error(SessionId, Message, Timestamp).
Decl frontend_ssr_error_candidate(SessionId, Message, Timestamp, ReqTs).
frontend_ssr_error_candidate(SessionId, Msg, Ts, ReqTs) :-
    frontend_ssr_error_with_key(Msg, Ts, KeyType, KeyValue),
    net_correlation_key(SessionId, ReqId, KeyType, KeyValue),
    net_request(SessionId, ReqId, _, _, _, ReqTs).

frontend_ssr_error(SessionId, Msg, Ts) :-
    frontend_ssr_error_candidate(SessionId, Msg, Ts, ReqTs),
    fn:minus(Ts, ReqTs) >= 0,
    fn:minus(Ts, ReqTs) < 5000.

# --- Derived: Python tracebacks (multi-line errors) ---
Decl python_traceback(Container, Message, Timestamp).
python_traceback(Container, Msg, Ts) :-
    docker_log(Container, "ERROR", "TRACEBACK", Msg, Ts).

# =============================================================================
# CROSS-LAYER CORRELATION RULES
# =============================================================================

# Rule: API failure correlates with backend error via shared correlation keys
# Links frontend API failures to backend exceptions using shared correlation keys.
Decl failed_api_request(SessionId, ReqId, Url, Status, ReqTs).
failed_api_request(SessionId, ReqId, Url, Status, ReqTs) :-
    net_request(SessionId, ReqId, _, Url, _, ReqTs),
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 400.

Decl failed_api_with_key(SessionId, ReqId, Url, Status, ReqTs, KeyType, KeyValue).
failed_api_with_key(SessionId, ReqId, Url, Status, ReqTs, KeyType, KeyValue) :-
    failed_api_request(SessionId, ReqId, Url, Status, ReqTs),
    net_correlation_key(SessionId, ReqId, KeyType, KeyValue).

Decl backend_error_with_key(BackendMsg, BackendTs, KeyType, KeyValue).
backend_error_with_key(BackendMsg, BackendTs, KeyType, KeyValue) :-
    backend_error(BackendMsg, BackendTs),
    docker_log_correlation("backend", KeyType, KeyValue, BackendMsg, BackendTs).

Decl api_backend_correlation(SessionId, ReqId, Url, Status, BackendMsg, TimeDelta).
api_backend_correlation(SessionId, ReqId, Url, Status, BackendMsg, TimeDelta) :-
    failed_api_with_key(SessionId, ReqId, Url, Status, ReqTs, KeyType, KeyValue),
    backend_error_with_key(BackendMsg, BackendTs, KeyType, KeyValue),
    TimeDelta = fn:minus(ReqTs, BackendTs).

# Rule: Console error traces to backend via API
# Full chain: Browser console error -> Failed API -> Backend exception
Decl full_stack_error(SessionId, ConsoleMsg, ReqId, Url, BackendMsg).
full_stack_error(SessionId, ConsoleMsg, ReqId, Url, BackendMsg) :-
    caused_by(SessionId, ConsoleMsg, ReqId),
    net_request(SessionId, ReqId, _, Url, _, _),
    api_backend_correlation(SessionId, ReqId, Url, _, BackendMsg, _).

# Rule: Backend errors without corresponding frontend errors (orphans)
# These indicate backend issues users haven't noticed yet
Decl orphan_backend_error(Message, Timestamp).
orphan_backend_error(Msg, Ts) :-
    backend_error(Msg, Ts).
# Note: Proper negation would need: !api_backend_correlation(_, _, _, Msg, _)
# But Mangle requires stratified negation - track orphans in Go code instead

# Rule: Frontend SSR errors correlate with hydration issues
# When Next.js server-side has errors, browser may see hydration mismatches
# Note: Using two rules for positive/negative delta since fn:abs not available
Decl ssr_hydration_correlation(SessionId, SsrMsg, ConsoleMsg, TimeDelta).
ssr_hydration_correlation(SessionId, SsrMsg, ConsoleMsg, TimeDelta) :-
    frontend_ssr_error(SessionId, SsrMsg, SsrTs),
    console_event(SessionId, "error", ConsoleMsg, ConsoleTs),
    fn:minus(ConsoleTs, SsrTs) >= 0,
    fn:minus(ConsoleTs, SsrTs) < 5000,
    TimeDelta = fn:minus(ConsoleTs, SsrTs).

ssr_hydration_correlation(SessionId, SsrMsg, ConsoleMsg, TimeDelta) :-
    frontend_ssr_error(SessionId, SsrMsg, SsrTs),
    console_event(SessionId, "error", ConsoleMsg, ConsoleTs),
    fn:minus(ConsoleTs, SsrTs) < 0,
    fn:minus(ConsoleTs, SsrTs) > -5000,
    TimeDelta = fn:minus(ConsoleTs, SsrTs).

# Rule: Slow API correlates with backend performance issues using shared keys.
Decl slow_api_request(SessionId, ReqId, Url, Duration, ReqTs).
slow_api_request(SessionId, ReqId, Url, Duration, ReqTs) :-
    slow_api(SessionId, ReqId, Url, Duration),
    net_request(SessionId, ReqId, _, _, _, ReqTs).

Decl slow_api_with_key(SessionId, ReqId, Url, Duration, ReqTs, KeyType, KeyValue).
slow_api_with_key(SessionId, ReqId, Url, Duration, ReqTs, KeyType, KeyValue) :-
    slow_api_request(SessionId, ReqId, Url, Duration, ReqTs),
    net_correlation_key(SessionId, ReqId, KeyType, KeyValue).

Decl slow_backend_correlation(SessionId, ReqId, Url, Duration, BackendMsg).
slow_backend_correlation(SessionId, ReqId, Url, Duration, BackendMsg) :-
    slow_api_with_key(SessionId, ReqId, Url, Duration, _, KeyType, KeyValue),
    docker_log_correlation("backend", KeyType, KeyValue, BackendMsg, _).

# =============================================================================
# ERROR PATTERN DETECTION
# =============================================================================

# Rule: Repeated errors (same message, multiple occurrences)
# Indicates systemic issues vs one-off failures
Decl repeated_backend_error(Message).
repeated_backend_error(Msg) :-
    backend_error(Msg, T1),
    backend_error(Msg, T2),
    T1 != T2.

# Rule: Auth-related errors (common pattern)
Decl auth_error_detected(Source, Message, Timestamp).
auth_error_detected("backend", Msg, Ts) :-
    backend_error(Msg, Ts).
# Go code should only push this fact if message contains auth keywords

# Rule: Database-related errors
Decl database_error_detected(Source, Message, Timestamp).
database_error_detected("backend", Msg, Ts) :-
    backend_error(Msg, Ts).
# Go code should only push this fact if message contains DB keywords

# =============================================================================
# CONTAINER HEALTH INDICATORS
# =============================================================================

# These are computed by Go code and pushed as facts for Mangle-based analysis
Decl container_health(Container, ErrorCount, WarningCount, Status).
# Status: "healthy", "degraded", "unhealthy"

# Rule: Any unhealthy container
Decl unhealthy_container(Container).
unhealthy_container(Container) :-
    container_health(Container, _, _, "unhealthy").

# Rule: Any degraded container
Decl degraded_container(Container).
degraded_container(Container) :-
    container_health(Container, _, _, "degraded").

# =============================================================================
# ROOT CAUSE ANALYSIS HELPERS
# =============================================================================

# Rule: Most likely root cause for a console error
# If we have full_stack_error, the backend message is the root cause
Decl root_cause(SessionId, ConsoleMsg, Source, Cause).
root_cause(SessionId, ConsoleMsg, "backend", BackendMsg) :-
    full_stack_error(SessionId, ConsoleMsg, _, _, BackendMsg).

root_cause_at(SessionId, ConsoleMsg, "backend", BackendMsg, ConsoleTs) :-
    full_stack_error(SessionId, ConsoleMsg, _, _, BackendMsg),
    console_event(SessionId, "error", ConsoleMsg, ConsoleTs).

slow_api_at(SessionId, ReqId, Url, Duration, ReqTs) :-
    slow_api(SessionId, ReqId, Url, Duration),
    net_request(SessionId, ReqId, _, _, _, ReqTs).

# Rule: Error requires investigation (no correlation found)
Decl unresolved_error(SessionId, Level, Message, Timestamp).
unresolved_error(SessionId, Level, Msg, Ts) :-
    console_event(SessionId, Level, Msg, Ts),
    Level = "error".
# Note: Would need negation for !caused_by to be truly "unresolved"
# Track in Go code by checking if caused_by returned empty

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

# =============================================================================
# VECTOR 14: SEMANTIC UI MACROS
# =============================================================================
# High-level semantic abstractions for common UI patterns, enabling agents
# to reason about "main content", "primary actions", and "obstructions".

# --- Screen Obstruction Detection ---
Decl screen_blocked(SessionId, NodeId, Reason).

screen_blocked(SessionId, Id, "modal") :- dom_attr(SessionId, Id, "class", "modal").
screen_blocked(SessionId, Id, "modal") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "modal").
screen_blocked(SessionId, Id, "modal-backdrop") :- dom_attr(SessionId, Id, "class", "modal-backdrop").
screen_blocked(SessionId, Id, "modal-backdrop") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "modal-backdrop").
screen_blocked(SessionId, Id, "dialog") :- dom_attr(SessionId, Id, "role", "dialog").
screen_blocked(SessionId, Id, "alertdialog") :- dom_attr(SessionId, Id, "role", "alertdialog").
screen_blocked(SessionId, Id, "loading-overlay") :-
    dom_attr(SessionId, Id, "id", "loading-overlay").
screen_blocked(SessionId, Id, "spinner") :- dom_attr(SessionId, Id, "class", "loading-spinner").
screen_blocked(SessionId, Id, "spinner") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "spinner").

# Derived: Page interaction is blocked
Decl interaction_blocked(SessionId, Reason).
interaction_blocked(SessionId, Reason) :-
    current_url(SessionId, _),
    screen_blocked(SessionId, _, Reason).

# --- Main Content Detection ---
Decl is_main_content(SessionId, NodeId).
is_main_content(SessionId, Id) :- dom_node(SessionId, Id, "main", _, _).
is_main_content(SessionId, Id) :- dom_attr(SessionId, Id, "id", "main").
is_main_content(SessionId, Id) :- dom_attr(SessionId, Id, "role", "main").
is_main_content(SessionId, Id) :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "main-content").

# --- Primary Action Detection ---
Decl primary_action(SessionId, Ref, Label).
primary_action(SessionId, Ref, Label) :- 
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "button_type", "submit").
primary_action(SessionId, Ref, Label) :- 
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "data_testid", TestID),
    :string:contains(TestID, "cta").
primary_action(SessionId, Ref, Label) :- 
    interactive(SessionId, Ref, "button", Label, _),
    elem_bbox(SessionId, Ref, _, _, W, H),
    W >= 200,
    H >= 40.
primary_action(SessionId, Ref, Label) :- 
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "id", Id),
    :string:contains(Id, "submit").

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
