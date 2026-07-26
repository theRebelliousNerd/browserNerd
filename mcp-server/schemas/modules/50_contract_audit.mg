# BrowserNERD schema module: frontend and API contract auditing
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 13b: FRONTEND/API CONTRACT AUDITING
# =============================================================================
# First slice of frontend-to-API contract auditing.
# AuthMechanism values: "jwt", "auth_header", "api_key", "none"
# Requirement values: "required", "optional"
#
# Performance note:
# Every derived rule anchors on a session/action/page-route-scoped frontend
# contract or a scoped request before joining backend declarations and applying
# negation, which keeps variable continuity intact and avoids cross-session
# Cartesian blowups.

# Frontend-declared expectations for a browser action that should call an API.
Decl scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, AuthMechanism).
Decl scoped_frontend_payload_requirement(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, Requirement).

# Backend-declared route contract expectations.
Decl scoped_backend_api_contract(SessionId, ApiRoute, Method, AuthMechanism).
Decl scoped_backend_payload_requirement(SessionId, ApiRoute, Method, Field, Requirement).

# Optional exact action-to-request correlation emitted by tooling when available.
Decl scoped_action_request_link(SessionId, ActionRef, RequestId).

# Observed request payload fields extracted from request bodies/forms.
Decl request_payload_field(SessionId, RequestId, Field, ValueKind).
Decl scoped_request_payload_field(SessionId, RequestId, Field, Value).
Decl scoped_request_payload_present(SessionId, RequestId, Field).
scoped_request_payload_present(SessionId, RequestId, Field) :-
    scoped_request_payload_field(SessionId, RequestId, Field, Value).
scoped_request_payload_present(SessionId, RequestId, Field) :-
    request_payload_field(SessionId, RequestId, Field, ValueKind).

# Unified action scope derived from browser interactions and the current page.
Decl scoped_observed_action(SessionId, ActionRef, PageRoute, ActionType, Timestamp).
scoped_observed_action(SessionId, Ref, PageRoute, "click", Ts) :-
    user_click(SessionId, Ref, Ts),
    current_url(SessionId, PageRoute).

scoped_observed_action(SessionId, Ref, PageRoute, "type", Ts) :-
    user_type(SessionId, Ref, _, Ts),
    current_url(SessionId, PageRoute).

scoped_observed_action(SessionId, Ref, PageRoute, "select", Ts) :-
    user_select(SessionId, Ref, _, Ts),
    current_url(SessionId, PageRoute).

scoped_observed_action(SessionId, Ref, PageRoute, "toggle", Ts) :-
    user_toggle(SessionId, Ref, Ts),
    current_url(SessionId, PageRoute).

# Request scope derived either from an exact action/request link or a bounded
# action->request timing window backed by the declared frontend contract.
Decl scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, RequestTs).
scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, RequestTs) :-
    scoped_action_request_link(SessionId, ActionRef, RequestId),
    scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, _),
    net_request(SessionId, RequestId, Method, ApiRoute, _, RequestTs).

scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, RequestTs) :-
    scoped_observed_action(SessionId, ActionRef, PageRoute, _, ActionTs),
    scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, _),
    net_request(SessionId, RequestId, Method, ApiRoute, _, RequestTs),
    RequestTs >= ActionTs,
    Delta = fn:minus(RequestTs, ActionTs),
    Delta < 5000.

# Contract helper predicates for safe negation.
Decl scoped_backend_contract_exists(SessionId, ApiRoute, Method).
scoped_backend_contract_exists(SessionId, ApiRoute, Method) :-
    scoped_backend_api_contract(SessionId, ApiRoute, Method, AuthMechanism).

Decl scoped_frontend_payload_declared(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field).
scoped_frontend_payload_declared(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field) :-
    scoped_frontend_payload_requirement(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, Requirement).

Decl scoped_frontend_required_payload_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field).
scoped_frontend_required_payload_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field) :-
    scoped_frontend_payload_requirement(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, "required").

Decl scoped_backend_payload_declared(SessionId, ApiRoute, Method, Field).
scoped_backend_payload_declared(SessionId, ApiRoute, Method, Field) :-
    scoped_backend_payload_requirement(SessionId, ApiRoute, Method, Field, Requirement).

Decl scoped_backend_required_payload(SessionId, ApiRoute, Method, Field).
scoped_backend_required_payload(SessionId, ApiRoute, Method, Field) :-
    scoped_backend_payload_requirement(SessionId, ApiRoute, Method, Field, "required").

# Observed auth evidence derived from normalized request headers.
Decl scoped_request_auth_header(SessionId, RequestId).
scoped_request_auth_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "authorization", Value).
scoped_request_auth_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "authorization", Value).

Decl scoped_request_jwt_bearer(SessionId, RequestId).
scoped_request_jwt_bearer(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "authorization", Value),
    :string:contains(Value, "Bearer ").
scoped_request_jwt_bearer(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "authorization", Value),
    :string:contains(Value, "Bearer ").

scoped_request_jwt_bearer(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "authorization", Value),
    :string:contains(Value, "bearer ").
scoped_request_jwt_bearer(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "authorization", Value),
    :string:contains(Value, "bearer ").

Decl scoped_request_api_key_header(SessionId, RequestId).
scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "x-api-key", Value).
scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "x-api-key", Value).

scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "api-key", Value).
scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "api-key", Value).

scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "apikey", Value).
scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "apikey", Value).

scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "request", "x-api_key", Value).
scoped_request_api_key_header(SessionId, RequestId) :-
    net_header(SessionId, RequestId, "req", "x-api_key", Value).

Decl scoped_request_satisfies_auth(SessionId, RequestId, Mechanism).
scoped_request_satisfies_auth(SessionId, RequestId, "auth_header") :-
    scoped_request_auth_header(SessionId, RequestId).

scoped_request_satisfies_auth(SessionId, RequestId, "jwt") :-
    scoped_request_jwt_bearer(SessionId, RequestId).

scoped_request_satisfies_auth(SessionId, RequestId, "api_key") :-
    scoped_request_api_key_header(SessionId, RequestId).

Decl scoped_request_observed_auth_mechanism(SessionId, RequestId, ObservedMechanism).
scoped_request_observed_auth_mechanism(SessionId, RequestId, "jwt") :-
    scoped_request_jwt_bearer(SessionId, RequestId),
    scoped_request_auth_header(SessionId, RequestId).

scoped_request_observed_auth_mechanism(SessionId, RequestId, "api_key") :-
    scoped_request_api_key_header(SessionId, RequestId).

scoped_request_observed_auth_mechanism(SessionId, RequestId, "auth_header") :-
    scoped_request_auth_header(SessionId, RequestId),
    !scoped_request_jwt_bearer(SessionId, RequestId).

# Derived audit findings
Decl scoped_missing_jwt_or_auth_header(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ExpectedMechanism).
scoped_missing_jwt_or_auth_header(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, "jwt") :-
    scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, _),
    scoped_backend_api_contract(SessionId, ApiRoute, Method, "jwt"),
    !scoped_request_satisfies_auth(SessionId, RequestId, "jwt").

scoped_missing_jwt_or_auth_header(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, "auth_header") :-
    scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, _),
    scoped_backend_api_contract(SessionId, ApiRoute, Method, "auth_header"),
    !scoped_request_satisfies_auth(SessionId, RequestId, "auth_header").

Decl scoped_missing_api_key(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method).
scoped_missing_api_key(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method) :-
    scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, _),
    scoped_backend_api_contract(SessionId, ApiRoute, Method, "api_key"),
    !scoped_request_satisfies_auth(SessionId, RequestId, "api_key").

Decl scoped_auth_mechanism_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ObservedMechanism, ExpectedMechanism).
scoped_auth_mechanism_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ObservedMechanism, ExpectedMechanism) :-
    scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, _),
    scoped_backend_api_contract(SessionId, ApiRoute, Method, ExpectedMechanism),
    scoped_request_observed_auth_mechanism(SessionId, RequestId, ObservedMechanism),
    ObservedMechanism != ExpectedMechanism,
    !scoped_request_satisfies_auth(SessionId, RequestId, ExpectedMechanism).

Decl scoped_payload_requirement_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, Field, Requirement).
scoped_payload_requirement_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, Field, "required") :-
    scoped_api_request(SessionId, ActionRef, PageRoute, RequestId, Method, ApiRoute, _),
    scoped_backend_required_payload(SessionId, ApiRoute, Method, Field),
    !scoped_request_payload_present(SessionId, RequestId, Field).

Decl scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Aspect, FrontendValue, BackendValue).
scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, "auth_mechanism", FrontendAuth, BackendAuth) :-
    scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, FrontendAuth),
    scoped_backend_api_contract(SessionId, ApiRoute, Method, BackendAuth),
    FrontendAuth != BackendAuth.

scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, "backend_contract", "declared", "missing") :-
    scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, FrontendAuth),
    !scoped_backend_contract_exists(SessionId, ApiRoute, Method).

scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, FrontendRequirement, BackendRequirement) :-
    scoped_frontend_payload_requirement(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, FrontendRequirement),
    scoped_backend_payload_requirement(SessionId, ApiRoute, Method, Field, BackendRequirement),
    FrontendRequirement != BackendRequirement.

scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, "missing", "required") :-
    scoped_frontend_api_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, FrontendAuth),
    scoped_backend_required_payload(SessionId, ApiRoute, Method, Field),
    !scoped_frontend_payload_declared(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field).

scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field, "required", "missing") :-
    scoped_frontend_required_payload_contract(SessionId, ActionRef, PageRoute, ApiRoute, Method, Field),
    !scoped_backend_payload_declared(SessionId, ApiRoute, Method, Field).
