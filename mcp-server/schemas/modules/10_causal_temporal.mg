# BrowserNERD schema module: causal, temporal, toast, and predictive reasoning
# Loaded through ../browser.mg in lexical order.

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
# DATALOG_MTL TEMPORAL MACROS (Temporal Ext)
# =============================================================================
Decl mt_click_event(SessionId, NodeId, Timestamp) temporal.
Decl mt_dom_node(SessionId, NodeId, Tag, Text, ParentId) temporal.
Decl mt_net_request(SessionId, ReqId, Context, Url, ParentId, Duration, Ts) temporal.
Decl mt_net_response(SessionId, ReqId, Status, Source, Duration, Ts) temporal.
Decl page_stable() temporal.

# DatalogMTL Rule: Network API has failed in recent temporal window
Decl recently_failed_api(SessionId, Url).
recently_failed_api(SessionId, Url) :-
   mt_net_request(SessionId, ReqId, _, Url, _, _, _)@[TReq],
   mt_net_response(SessionId, ReqId, Status, _, _, _)@[TRes],
   Status >= 400,
   :time:ge(TRes, TReq),
   Diff = fn:time:sub(TRes, TReq),
   Limit = fn:duration:parse('30s'),
   :duration:le(Diff, Limit).

# =============================================================================
# PREDICTIVE ASSERTIONS (Contract Engine)
# =============================================================================

# A task expects a UI response (like a modal appearing) within 2 seconds of a click
Decl modal_expected(SessionId, NodeId).
modal_expected(SessionId, ClickNodeId) :-
    mt_click_event(SessionId, ClickNodeId, _)@[TClick],
    <+[0s, 2s] mt_dom_node(SessionId, _, "dialog", _, _).

# Expect the page to maintain stability without requests for the next 1 second
Decl stable_expected(SessionId).
stable_expected(SessionId) :-
    current_url(SessionId, _),
    [+[0s, 1s] page_stable().

# =============================================================================
# EXTERNAL PREDICATES (Custom Transform Functions)
# =============================================================================
Decl my_distance(X1, Y1, X2, Y2, Dist)
  descr [
      external(),
      mode('-', '-', '-', '-', '-')
  ]
  bound [ /number, /number, /number, /number, /number ].

# Detect if elements are far apart
Decl elements_far_apart(SessionId, NodeId1, NodeId2, Dist).
elements_far_apart(SessionId, NodeId1, NodeId2, Dist) :-
    dom_layout(SessionId, NodeId1, X1, Y1, _, _, _),
    dom_layout(SessionId, NodeId2, X2, Y2, _, _, _),
    my_distance(X1, Y1, X2, Y2, Dist),
    Dist > 100.0.

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
    TDiff = fn:minus(TError, TNet),
    TDiff < 100.

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
