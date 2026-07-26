package mangle

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func newContractAuditEngine(t *testing.T) *Engine {
	t.Helper()

	engine, err := NewEngine(config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	return engine
}

func contractFact(ts int64, predicate string, args ...interface{}) Fact {
	return Fact{
		Predicate: predicate,
		Args:      args,
		Timestamp: time.UnixMilli(ts),
	}
}

func requireDerivedFact(t *testing.T, facts []Fact, args ...interface{}) {
	t.Helper()

	for _, fact := range facts {
		if len(fact.Args) != len(args) {
			continue
		}

		match := true
		for i, want := range args {
			if !valuesEquivalent(fact.Args[i], want) {
				match = false
				break
			}
		}
		if match {
			return
		}
	}

	t.Fatalf("expected derived fact with args %v, got %+v", args, facts)
}

func TestContractAuditMissingJWTOrAuthHeader(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(900, "scoped_frontend_api_contract", "session-contract-jwt", "save-profile", "/profile", "/api/profile", "POST", "jwt"),
		contractFact(900, "scoped_backend_api_contract", "session-contract-jwt", "/api/profile", "POST", "jwt"),
		contractFact(950, "current_url", "session-contract-jwt", "/profile"),
		contractFact(1000, "user_click", "session-contract-jwt", "save-profile", int64(1000)),
		contractFact(1100, "net_request", "session-contract-jwt", "req-jwt", "POST", "/api/profile", "fetch", int64(1100)),
		contractFact(900, "scoped_frontend_api_contract", "session-contract-jwt-2", "save-profile", "/profile", "/api/profile", "POST", "jwt"),
		contractFact(900, "scoped_backend_api_contract", "session-contract-jwt-2", "/api/profile", "POST", "jwt"),
		contractFact(950, "current_url", "session-contract-jwt-2", "/profile"),
		contractFact(1000, "user_click", "session-contract-jwt-2", "save-profile", int64(1000)),
		contractFact(1100, "net_request", "session-contract-jwt-2", "req-jwt-2", "POST", "/api/profile", "fetch", int64(1100)),
		contractFact(1110, "net_header", "session-contract-jwt-2", "req-jwt-2", "request", "authorization", "Bearer secondary-token"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	results, err := engine.Evaluate(ctx, "scoped_missing_jwt_or_auth_header")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	requireDerivedFact(
		t,
		results,
		"session-contract-jwt",
		"save-profile",
		"/profile",
		"req-jwt",
		"/api/profile",
		"POST",
		"jwt",
	)
}

func TestContractAuditMissingAPIKey(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(1900, "scoped_frontend_api_contract", "session-contract-key", "refresh-metrics", "/ops", "/api/metrics", "GET", "api_key"),
		contractFact(1900, "scoped_backend_api_contract", "session-contract-key", "/api/metrics", "GET", "api_key"),
		contractFact(1950, "current_url", "session-contract-key", "/ops"),
		contractFact(2000, "user_click", "session-contract-key", "refresh-metrics", int64(2000)),
		contractFact(2050, "net_request", "session-contract-key", "req-key", "GET", "/api/metrics", "fetch", int64(2050)),
		contractFact(2060, "net_header", "session-contract-key", "req-key", "request", "authorization", "Bearer admin-token"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	results, err := engine.Evaluate(ctx, "scoped_missing_api_key")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	requireDerivedFact(
		t,
		results,
		"session-contract-key",
		"refresh-metrics",
		"/ops",
		"req-key",
		"/api/metrics",
		"GET",
	)
}

func TestContractAuditAuthMechanismMismatch(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(2900, "scoped_frontend_api_contract", "session-contract-auth-mismatch", "refresh-metrics", "/ops", "/api/metrics", "GET", "jwt"),
		contractFact(2900, "scoped_backend_api_contract", "session-contract-auth-mismatch", "/api/metrics", "GET", "api_key"),
		contractFact(2950, "current_url", "session-contract-auth-mismatch", "/ops"),
		contractFact(3000, "user_click", "session-contract-auth-mismatch", "refresh-metrics", int64(3000)),
		contractFact(3050, "net_request", "session-contract-auth-mismatch", "req-auth-mismatch", "GET", "/api/metrics", "fetch", int64(3050)),
		contractFact(3060, "net_header", "session-contract-auth-mismatch", "req-auth-mismatch", "request", "authorization", "Bearer admin-token"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	results, err := engine.Evaluate(ctx, "scoped_auth_mechanism_mismatch")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	requireDerivedFact(
		t,
		results,
		"session-contract-auth-mismatch",
		"refresh-metrics",
		"/ops",
		"req-auth-mismatch",
		"/api/metrics",
		"GET",
		"jwt",
		"api_key",
	)
}

func TestContractAuditPayloadRequirementMismatch(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(3900, "scoped_frontend_api_contract", "session-contract-payload", "submit-order", "/checkout", "/api/orders", "POST", "jwt"),
		contractFact(3900, "scoped_backend_api_contract", "session-contract-payload", "/api/orders", "POST", "jwt"),
		contractFact(3900, "scoped_backend_payload_requirement", "session-contract-payload", "/api/orders", "POST", "orderId", "required"),
		contractFact(3950, "current_url", "session-contract-payload", "/checkout"),
		contractFact(4000, "user_click", "session-contract-payload", "submit-order", int64(4000)),
		contractFact(4050, "net_request", "session-contract-payload", "req-order", "POST", "/api/orders", "fetch", int64(4050)),
		contractFact(4060, "net_header", "session-contract-payload", "req-order", "request", "authorization", "Bearer checkout-token"),
		contractFact(4070, "scoped_request_payload_field", "session-contract-payload", "req-order", "cartId", "cart-123"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	results, err := engine.Evaluate(ctx, "scoped_payload_requirement_mismatch")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	requireDerivedFact(
		t,
		results,
		"session-contract-payload",
		"submit-order",
		"/checkout",
		"req-order",
		"/api/orders",
		"POST",
		"orderId",
		"required",
	)
}

func TestContractAuditFrontendBackendContractGap(t *testing.T) {
	engine := newContractAuditEngine(t)
	ctx := context.Background()

	facts := []Fact{
		contractFact(4900, "scoped_frontend_api_contract", "session-contract-gap", "load-users", "/admin", "/api/admin/users", "GET", "jwt"),
		contractFact(4900, "scoped_backend_api_contract", "session-contract-gap", "/api/admin/users", "GET", "api_key"),
		contractFact(4900, "scoped_frontend_payload_requirement", "session-contract-gap", "load-users", "/admin", "/api/admin/users", "GET", "tenantId", "optional"),
		contractFact(4900, "scoped_backend_payload_requirement", "session-contract-gap", "/api/admin/users", "GET", "tenantId", "required"),
		contractFact(4900, "scoped_frontend_api_contract", "session-contract-gap", "load-audit", "/admin", "/api/admin/audit", "GET", "jwt"),
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	results, err := engine.Evaluate(ctx, "scoped_frontend_backend_contract_gap")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	requireDerivedFact(
		t,
		results,
		"session-contract-gap",
		"load-users",
		"/admin",
		"/api/admin/users",
		"GET",
		"auth_mechanism",
		"jwt",
		"api_key",
	)

	requireDerivedFact(
		t,
		results,
		"session-contract-gap",
		"load-users",
		"/admin",
		"/api/admin/users",
		"GET",
		"tenantId",
		"optional",
		"required",
	)

	requireDerivedFact(
		t,
		results,
		"session-contract-gap",
		"load-audit",
		"/admin",
		"/api/admin/audit",
		"GET",
		"backend_contract",
		"declared",
		"missing",
	)
}
