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
# A failed app request: any 5xx, or a 4xx that is not a page asset (a missing
# favicon, image, font or stylesheet is noise, not a broken feature).
Decl failed_request(SessionId, RequestId, Url, Status).
Decl failed_request_at(SessionId, RequestId, Url, Status, RequestTimestamp).
# The same failure stamped when its response arrived (net_http_error).
Decl failed_request_done(SessionId, RequestId, Url, Status, DoneTimestamp).
# A request that failed below HTTP (refused, DNS, CORS), not an abort.
Decl network_failure(SessionId, RequestId, Url, ErrorText, Timestamp).
# CDP resource types of page assets.
Decl asset_resource_type(ResourceType).
Decl asset_http_error(SessionId, RequestId).
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
# The engine mirrors a base fact as mt_<predicate> at its timestamp only for
# the mt_ predicates declared here.
Decl mt_click_event(SessionId, NodeId, Timestamp) temporal.
Decl mt_dom_node(SessionId, NodeId, Tag, Text, ParentId) temporal.
Decl page_stable() temporal.

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

# my_distance is for queries that bind both points first. No schema rule joins
# dom_layout with itself: a page's element pairs grow with the square of its
# element count, passed the derived fact ceiling on a login page, and starved
# every later fact insertion (internal/mangle/layout_scaling_test.go).

# =============================================================================
# CAUSAL REASONING RULES (PRD Section 3.4)
# =============================================================================

# Time-window correlations join on a time bucket as well as the session, so
# a join only meets events in the same or the previous bucket. Joining two
# event streams on the session alone and filtering by time afterwards builds
# their cross product first: under a failing poll that passed the created-fact
# limit within minutes and aborted every evaluation.

# Rule 1: API-Triggered Crash Detection
# A console error is caused by the LAST request that failed (an HTTP error or
# a network failure) before it, at most 2 s before. Under a failing poll
# several failures fall inside the window; only the nearest one is the cause.
Decl console_error_bucket(SessionId, Bucket, Message, Timestamp).
console_error_bucket(SessionId, Bucket, Msg, T) :-
    console_event(SessionId, "error", Msg, T),
    Bucket = fn:div(T, 2000).

Decl request_failure_bucket(SessionId, Bucket, RequestId, Timestamp).
request_failure_bucket(SessionId, Bucket, ReqId, T) :-
    failed_request_done(SessionId, ReqId, _, _, T),
    Bucket = fn:div(T, 2000).
request_failure_bucket(SessionId, Bucket, ReqId, T) :-
    network_failure(SessionId, ReqId, _, _, T),
    Bucket = fn:div(T, 2000).

# The engine indexes the first two arguments, so helper predicates put their
# join key (session plus bucket or timestamp) there.
Decl caused_by_candidate(SessionId, ErrorTimestamp, ConsoleErr, RequestId, FailTimestamp).
caused_by_candidate(SessionId, TError, ConsoleErr, ReqId, TFail) :-
    console_error_bucket(SessionId, Bucket, ConsoleErr, TError),
    request_failure_bucket(SessionId, Bucket, ReqId, TFail),
    TFail <= TError,
    TDiff = fn:minus(TError, TFail),
    TDiff < 2000.

caused_by_candidate(SessionId, TError, ConsoleErr, ReqId, TFail) :-
    console_error_bucket(SessionId, Bucket, ConsoleErr, TError),
    Previous = fn:minus(Bucket, 1),
    request_failure_bucket(SessionId, Previous, ReqId, TFail),
    TFail <= TError,
    TDiff = fn:minus(TError, TFail),
    TDiff < 2000.

Decl caused_by_nearest(SessionId, ErrorTimestamp, ConsoleErr, FailTimestamp).
caused_by_nearest(SessionId, TError, ConsoleErr, Nearest) :-
    caused_by_candidate(SessionId, TError, ConsoleErr, _, TFail) |>
    do fn:group_by(SessionId, TError, ConsoleErr),
    let Nearest = fn:max(TFail).

caused_by(SessionId, ConsoleErr, ReqId) :-
    caused_by_nearest(SessionId, TError, ConsoleErr, TFail),
    caused_by_candidate(SessionId, TError, ConsoleErr, ReqId, TFail).

# Rule 2: Slow API Detection (>1 second from request to response headers)
# Flags API calls exceeding performance SLA
slow_api(SessionId, ReqId, Url, Duration) :-
    net_response(SessionId, ReqId, _, _, Duration),
    Duration > 1000,
    net_request(SessionId, ReqId, _, Url, _, _).

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
# Every 5xx, and every 4xx except a page asset's.
asset_resource_type("Image").
asset_resource_type("Font").
asset_resource_type("Stylesheet").
asset_resource_type("Media").
asset_resource_type("Manifest").
asset_resource_type("TextTrack").
asset_resource_type("Other").

asset_http_error(SessionId, ReqId) :-
    net_http_error(SessionId, ReqId, _, _, Type, _),
    asset_resource_type(Type).

# Responses first: the status filter leaves only failures to join.
failed_request(SessionId, ReqId, Url, Status) :-
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 500,
    net_request(SessionId, ReqId, _, Url, _, _).

failed_request(SessionId, ReqId, Url, Status) :-
    net_response(SessionId, ReqId, Status, _, _),
    Status >= 400,
    Status < 500,
    net_request(SessionId, ReqId, _, Url, _, _),
    !asset_http_error(SessionId, ReqId).

failed_request_at(SessionId, ReqId, Url, Status, ReqTs) :-
    net_request(SessionId, ReqId, _, Url, _, ReqTs),
    failed_request(SessionId, ReqId, Url, Status).

failed_request_done(SessionId, ReqId, Url, Status, DoneTs) :-
    net_http_error(SessionId, ReqId, Url, Status, _, DoneTs),
    Status >= 500.

failed_request_done(SessionId, ReqId, Url, Status, DoneTs) :-
    net_http_error(SessionId, ReqId, Url, Status, Type, DoneTs),
    Status < 500,
    !asset_resource_type(Type).

network_failure(SessionId, ReqId, Url, ErrorText, T) :-
    net_loading_failed(SessionId, ReqId, ErrorText, "false", T),
    net_request(SessionId, ReqId, _, Url, _, _).

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
# Correlates an error toast with the app request that failed nearest to it,
# at most 5 s earlier. Toast times come from the page clock and response
# times from the CDP event, so the toast may read up to 500 ms before the
# response; nearness is the squared difference.
Decl error_toast_bucket(SessionId, Bucket, Text, Timestamp).
error_toast_bucket(SessionId, Bucket, Text, T) :-
    error_toast(SessionId, Text, _, T),
    Bucket = fn:div(T, 5000).

Decl failed_request_bucket(SessionId, Bucket, RequestId, Url, Status, Timestamp).
failed_request_bucket(SessionId, Bucket, ReqId, Url, Status, T) :-
    failed_request_done(SessionId, ReqId, Url, Status, T),
    Bucket = fn:div(T, 5000).

Decl toast_failure_candidate(SessionId, ToastTimestamp, Text, RequestId, Url, Status, TimeDelta, Distance).
toast_failure_candidate(SessionId, TToast, ToastText, ReqId, Url, Status, TimeDelta, Distance) :-
    error_toast_bucket(SessionId, Bucket, ToastText, TToast),
    failed_request_bucket(SessionId, Bucket, ReqId, Url, Status, TFail),
    TimeDelta = fn:minus(TToast, TFail),
    TimeDelta > -500,
    TimeDelta < 5000,
    Distance = fn:mult(TimeDelta, TimeDelta).

toast_failure_candidate(SessionId, TToast, ToastText, ReqId, Url, Status, TimeDelta, Distance) :-
    error_toast_bucket(SessionId, Bucket, ToastText, TToast),
    Previous = fn:minus(Bucket, 1),
    failed_request_bucket(SessionId, Previous, ReqId, Url, Status, TFail),
    TimeDelta = fn:minus(TToast, TFail),
    TimeDelta > -500,
    TimeDelta < 5000,
    Distance = fn:mult(TimeDelta, TimeDelta).

toast_failure_candidate(SessionId, TToast, ToastText, ReqId, Url, Status, TimeDelta, Distance) :-
    error_toast_bucket(SessionId, Bucket, ToastText, TToast),
    Next = fn:plus(Bucket, 1),
    failed_request_bucket(SessionId, Next, ReqId, Url, Status, TFail),
    TimeDelta = fn:minus(TToast, TFail),
    TimeDelta > -500,
    TimeDelta < 5000,
    Distance = fn:mult(TimeDelta, TimeDelta).

Decl toast_failure_nearest(SessionId, ToastTimestamp, Text, Distance).
toast_failure_nearest(SessionId, TToast, ToastText, Nearest) :-
    toast_failure_candidate(SessionId, TToast, ToastText, _, _, _, _, Distance) |>
    do fn:group_by(SessionId, TToast, ToastText),
    let Nearest = fn:min(Distance).

toast_after_api_failure(SessionId, ToastText, ReqId, Url, Status, TimeDelta) :-
    toast_failure_nearest(SessionId, TToast, ToastText, Nearest),
    toast_failure_candidate(SessionId, TToast, ToastText, ReqId, Url, Status, TimeDelta, Nearest).

# Rule 8: User Visible Errors (unified view)
# Aggregates all user-visible errors from different sources
user_visible_error(SessionId, "toast", Msg, Ts) :-
    error_toast(SessionId, Msg, _, Ts).

user_visible_error(SessionId, "console", Msg, Ts) :-
    console_event(SessionId, "error", Msg, Ts).

# Rule 9: Repeated Toast Errors
# The same error toast more than once (a systemic issue). Counted, not
# self-joined: a toast repeated k times would otherwise build k*k pairs.
Decl error_toast_count(SessionId, Message, Count).
error_toast_count(SessionId, Msg, Count) :-
    error_toast(SessionId, Msg, _, _) |>
    do fn:group_by(SessionId, Msg),
    let Count = fn:count().

repeated_toast_error(SessionId, Msg) :-
    error_toast_count(SessionId, Msg, Count),
    Count > 1.

# Rule 10: Toast Error Chain
# Full chain: Error toast -> Failed API -> URL and status
# Similar to error_chain but for toast-based detection
toast_error_chain(SessionId, ToastText, ReqId, Url, Status) :-
    toast_after_api_failure(SessionId, ToastText, ReqId, Url, Status, _).

# Rule 11: Toast Without API Correlation
# Detects error toasts that don't correlate with any API failure
# (could indicate client-side validation errors or other issues)
# Note: Requires tracking in Go code since Mangle negation is limited
