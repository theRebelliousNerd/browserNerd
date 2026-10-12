# BrowserNERD schema module: interaction, automation, login, and declarative testing
# Loaded through ../browser.mg in lexical order.

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
# Emitted by browser-act's upload op: FileCount files set on a file input.
Decl user_upload(SessionId, Ref, FileCount, Timestamp).

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

# Rule: Form ready for submission (two or more fields filled)
Decl form_fill_count(SessionId, Count).
form_fill_count(SessionId, Count) :-
    form_field_filled(SessionId, _, _) |>
    do fn:group_by(SessionId),
    let Count = fn:count().

Decl form_ready(SessionId).
form_ready(SessionId) :-
    form_fill_count(SessionId, Count),
    Count > 1.

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
# Detects when the URL changed within 5 s after a form submission (universal
# pattern). Navigations join on a 5 s bucket, not on the whole session.
Decl navigation_bucket(SessionId, Bucket, Url, Timestamp).
navigation_bucket(SessionId, Bucket, Url, T) :-
    navigation_event(SessionId, Url, T),
    Bucket = fn:div(T, 5000).

Decl submit_from_url(SessionId, UrlBefore, TSubmit, Bucket).
submit_from_url(SessionId, UrlBefore, TSubmit, Bucket) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, UrlBefore, TBefore),
    TSubmit >= TBefore,
    Bucket = fn:div(TSubmit, 5000).

Decl url_changed_after_submit(SessionId, UrlBefore, UrlAfter, TNav).
url_changed_after_submit(SessionId, UrlBefore, UrlAfter, TNav) :-
    submit_from_url(SessionId, UrlBefore, TSubmit, Bucket),
    navigation_bucket(SessionId, Bucket, UrlAfter, TNav),
    TNav >= TSubmit,
    Delta = fn:minus(TNav, TSubmit),
    Delta < 5000,
    UrlBefore != UrlAfter.

url_changed_after_submit(SessionId, UrlBefore, UrlAfter, TNav) :-
    submit_from_url(SessionId, UrlBefore, TSubmit, Bucket),
    Next = fn:plus(Bucket, 1),
    navigation_bucket(SessionId, Next, UrlAfter, TNav),
    TNav >= TSubmit,
    Delta = fn:minus(TNav, TSubmit),
    Delta < 5000,
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
    TimeDelta = fn:minus(TNav, TSubmit),
    TimeDelta < 5000.

# --- Alternative: Navigation-only success (no POST required) ---
# Some sites use client-side routing without a POST (OAuth redirects, etc.)
# This fires if URL changes after submit, even without a successful POST
Decl login_succeeded_navigation_only(SessionId).
login_succeeded_navigation_only(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    url_changed_after_submit(SessionId, _, _, TNav),
    TimeDelta = fn:minus(TNav, TSubmit),
    TimeDelta < 5000.

# --- Login failure detection ---
# Detects when form was submitted but URL didn't change (stayed on login page)
# or when there was a failed API response
Decl login_failed_no_navigation(SessionId).
login_failed_no_navigation(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, UrlBefore, TBefore),
    TSubmit >= TBefore,
    current_url(SessionId, UrlBefore).

Decl login_failed_api_error(SessionId, ReqId, Status).
login_failed_api_error(SessionId, ReqId, Status) :-
    failed_request_at(SessionId, ReqId, _, Status, TReq),
    net_request(SessionId, ReqId, "POST", _, _, _),
    form_submitted(SessionId, _, TSubmit),
    TReq >= TSubmit,
    TimeDelta = fn:minus(TReq, TSubmit),
    TimeDelta < 2000.

# --- Session state tracking for login context ---
# Tracks that we're in a "login attempt" state (form submitted, awaiting result)
Decl login_attempt_pending(SessionId, Timestamp).
login_attempt_pending(SessionId, TSubmit) :-
    form_submitted(SessionId, _, TSubmit),
    url_before_submit(SessionId, _, TBefore),
    TSubmit >= TBefore.

# --- Authenticated session indicator ---
# Generic indicator that can be filled by code-level detection
# (e.g., detecting auth cookies, JWT tokens, or session storage)
Decl authenticated_session(SessionId, Method, Timestamp).

# Rule: Failed API call during automation
Decl automation_error(SessionId, ReqId, Url).
automation_error(SessionId, ReqId, Url) :-
    failed_request_at(SessionId, ReqId, Url, _, TReq),
    plan_executed(SessionId, _, _, Failed, TPlan),
    Failed > 0,
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
