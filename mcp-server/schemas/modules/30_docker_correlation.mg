# BrowserNERD schema module: Docker and cross-layer error correlation
# Loaded through ../browser.mg in lexical order.

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
    TimeDelta = fn:minus(Ts, ReqTs),
    TimeDelta >= 0,
    TimeDelta < 5000.

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
    TimeDelta = fn:minus(ConsoleTs, SsrTs),
    TimeDelta >= 0,
    TimeDelta < 5000.

ssr_hydration_correlation(SessionId, SsrMsg, ConsoleMsg, TimeDelta) :-
    frontend_ssr_error(SessionId, SsrMsg, SsrTs),
    console_event(SessionId, "error", ConsoleMsg, ConsoleTs),
    TimeDelta = fn:minus(ConsoleTs, SsrTs),
    TimeDelta < 0,
    TimeDelta > -5000.

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
