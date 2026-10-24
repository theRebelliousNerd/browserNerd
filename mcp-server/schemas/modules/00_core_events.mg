# BrowserNERD schema module: core browser, React, DOM, network, and event facts
# Loaded through ../browser.mg in lexical order.

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
# 1. No network requests in last 500ms
# A page is stable if:
# 1. No network requests in last 500ms
# 2. No DOM updates in last 200ms
# (Note: This is a conceptual rule; the tool will implement the logic using temporal queries)


# --- Network Events (HAR-like schema per PRD Section 3.3) ---
# Core transaction record
# Session-scoped to prevent cross-session contamination.
Decl net_request(SessionId, Id, Method, Url, InitiatorId, StartTime).
# CDP request identity; loaders distinguish documents sharing a URL.
Decl net_request_context(SessionId, Id, ResourceType, FrameId, LoaderId).
# Response metadata with timing, whole milliseconds: Latency is the server's
# wait (request sent to first byte); Duration is request issued to response
# headers received, queueing and connection setup included.
Decl net_response(SessionId, Id, Status, Latency, Duration).
# Normalized headers (keys lowercased)
Decl net_header(SessionId, Id, Kind, Key, Value).
# Correlation keys normalized from headers (request_id/correlation_id/trace_id)
Decl net_correlation_key(SessionId, Id, KeyType, KeyValue).
# Critical for causality: what triggered this request?
Decl request_initiator(SessionId, Id, Type, ScriptId).
# A response with Status >= 400, stamped when it arrived. ResourceType is the
# CDP type (Document, XHR, Fetch, Script, Image, Font, Stylesheet, ...).
Decl net_http_error(SessionId, Id, Url, Status, ResourceType, Timestamp).
# A request that got no response: refused connection, DNS, CORS, abort.
# Canceled is "true" for aborted requests (navigation away, AbortController).
Decl net_loading_failed(SessionId, Id, ErrorText, Canceled, Timestamp).
# The first KB of a failed (>= 400) text/JSON response body, redacted.
Decl net_failure_body(SessionId, Id, Snippet).
# WebSocket lifecycle; Event is created | handshake | open | closed | error.
# Frame payloads are never captured.
Decl ws_event(SessionId, WsId, Url, Event, Detail, Timestamp).

# --- Browser/User Events ---
# Session-scoped to prevent cross-session contamination.
Decl console_event(SessionId, Level, Message, Timestamp).
Decl click_event(SessionId, NodeId, Timestamp).
Decl input_event(SessionId, NodeId, Value, Timestamp).
Decl state_change(SessionId, Name, Value, Timestamp).
Decl navigation_event(SessionId, Url, Timestamp).
# Main-frame navigation kind (document | same_document) and document loader.
Decl navigation_context(SessionId, Url, Kind, LoaderId, Timestamp).

# Browser-generated warnings and errors (deprecation, intervention, security,
# CSP, violation); Level is warning | error.
Decl browser_log(SessionId, Source, Level, Text, Url, Timestamp).
# A JavaScript alert/confirm/prompt/beforeunload, answered the moment it
# opened. Accepted is "true"/"false"; HandledBy is armed | policy.
Decl js_dialog(SessionId, Type, Message, Accepted, HandledBy, Timestamp).
# A download; State is started | completed | canceled.
Decl download(SessionId, Guid, Url, SuggestedName, State, Timestamp).

# --- Session State (for current_url predicate) ---
# The engine keeps only the latest current_url per session.
Decl current_url(SessionId, Url).
# The main frame failed to load Url: a network error, an HTTP >= 400
# document, or Chrome's error page committed in its place.
Decl page_load_failed(SessionId, Url, ErrorText, Timestamp).

# --- Toast/Notification Events (instant error overlay detection) ---
# Captured via MutationObserver watching for toast elements in real-time
# Session-scoped to prevent cross-session contamination.
Decl toast_notification(SessionId, Text, Level, Source, Timestamp).
# Level: "error", "warning", "success", "info"
# Source: UI library (material-ui, chakra-ui, ant-design, shadcn, react-toastify, react-hot-toast, notistack, native)

# Convenience predicates for level-specific queries
Decl error_toast(SessionId, Text, Source, Timestamp).
Decl warning_toast(SessionId, Text, Source, Timestamp).

# Generic ARIA announcements, including inline validation and status badges.
# Role does not establish severity; toast markers are classified separately.
Decl live_region_notification(SessionId, Text, Role, Level, Timestamp).
