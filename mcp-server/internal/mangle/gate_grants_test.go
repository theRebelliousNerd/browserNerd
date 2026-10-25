package mangle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func grantTestEngine(limits gateGrantLimits, clock func() time.Time) *Engine {
	book := newGateGrantLedger(limits)
	book.clock = clock
	return &Engine{cfg: config.MangleConfig{Enable: true}, gateGrants: book}
}

func TestGateGrantsExactScopeRefreshAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	e := grantTestEngine(gateGrantLimits{2, 4, 4096}, func() time.Time { return now })
	expiry, err := e.PublishReasonGrants(context.Background(), "session", []string{"reason:network", "reason:network"}, []string{"low_confidence"})
	if err != nil || expiry != now.Add(GateGrantTTL) || e.gateGrants.keys != 2 {
		t.Fatalf("issue: %v %+v", err, e.gateGrants)
	}
	if !e.HasGateHandle("session", "reason:network") || !e.HasGateReason("session", "low_confidence") {
		t.Fatal("actual issued keys unavailable")
	}
	if e.HasGateHandle("foreign", "reason:network") || e.HasGateHandle("session", "low_confidence") || e.HasGateReason("session", "reason:network") {
		t.Fatal("tuple/kind boundary crossed")
	}
	now = now.Add(9 * time.Minute)
	if _, err := e.PublishReasonGrants(context.Background(), "session", []string{"reason:network"}, nil); err != nil {
		t.Fatal(err)
	}
	if e.gateGrants.keys != 2 {
		t.Fatal("refresh consumed a new key")
	}
	now = now.Add(2 * time.Minute)
	if !e.HasGateHandle("session", "reason:network") || e.HasGateReason("session", "low_confidence") {
		t.Fatal("refresh extended unrelated reason or lost exact handle")
	}
	now = now.Add(8 * time.Minute)
	if e.HasGateHandle("session", "reason:network") || len(e.gateGrants.sessions) != 0 || e.gateGrants.keys != 0 || e.gateGrants.bytes != 0 {
		t.Fatal("expiry boundary/cleanup accounting is wrong")
	}
}

func TestGateGrantsCapacityAtomicNoLiveEviction(t *testing.T) {
	for _, item := range []struct {
		name   string
		limits gateGrantLimits
	}{
		{"sessions", gateGrantLimits{1, 8, 4096}},
		{"keys", gateGrantLimits{8, 1, 4096}},
		{"bytes", gateGrantLimits{8, 8, len("first") + 64 + len("reason:a") + 64}},
	} {
		t.Run(item.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			e := grantTestEngine(item.limits, func() time.Time { return now })
			expiry, err := e.PublishReasonGrants(context.Background(), "first", []string{"reason:a"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			oldBytes := e.gateGrants.bytes
			now = now.Add(time.Minute)
			if _, err = e.PublishReasonGrants(context.Background(), "other", []string{"reason:b"}, nil); !errors.Is(err, ErrGateGrantCapacity) {
				t.Fatalf("missing capacity refusal: %v", err)
			}
			if !e.HasGateHandle("first", "reason:a") || e.HasGateHandle("other", "reason:b") || e.gateGrants.bytes != oldBytes {
				t.Fatal("capacity refusal changed live grants")
			}
			if item.name != "sessions" {
				if _, err = e.PublishReasonGrants(context.Background(), "first", []string{"reason:a", "reason:new"}, nil); !errors.Is(err, ErrGateGrantCapacity) {
					t.Fatal("bundle must refuse atomically")
				}
				if e.gateGrants.sessions["first"][gateGrantKey{'h', "reason:a"}] != expiry {
					t.Fatal("failed bundle partially refreshed existing key")
				}
			}
			if _, err = e.PublishReasonGrants(context.Background(), "first", []string{"reason:a"}, nil); err != nil {
				t.Fatal("exact refresh should fit a full ledger")
			}
			now = now.Add(GateGrantTTL)
			if _, err = e.PublishReasonGrants(context.Background(), "other", []string{"reason:b"}, nil); err != nil {
				t.Fatalf("expired capacity not reclaimed: %v", err)
			}
		})
	}
}

func TestGateGrantsValidationCancellationAndByteBounds(t *testing.T) {
	now := time.Now()
	e := grantTestEngine(gateGrantLimits{256, 4096, GateGrantMaxBytes}, func() time.Time { return now })
	for _, test := range []struct {
		s    string
		h, r []string
	}{
		{"", []string{"reason:network"}, nil}, {" space ", []string{"reason:network"}, nil},
		{strings.Repeat("s", 257), []string{"reason:network"}, nil}, {"s", []string{"observe:s:state"}, nil},
		{"s", []string{"reason:"}, nil}, {"s", []string{"reason:" + strings.Repeat("x", 250)}, nil},
		{"s", []string{"reason:network"}, []string{"explicit_user_intent"}}, {"s", nil, []string{"unknown"}},
	} {
		if _, err := e.PublishReasonGrants(context.Background(), test.s, test.h, test.r); !errors.Is(err, ErrGateGrantInvalid) {
			t.Fatalf("invalid bundle accepted: %v", err)
		}
	}
	if len(e.gateGrants.sessions) != 0 {
		t.Fatal("invalid bundle mutated authority")
	}
	if _, err := e.PublishReasonGrants(context.Background(), "s", nil, nil); !errors.Is(err, ErrGateGrantEmpty) {
		t.Fatal("empty bundle claimed authorization")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.PublishReasonGrants(ctx, "s", []string{"reason:network"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled issuer mutated authority")
	}
	if e.HasGateHandle("s", "reason:network") {
		t.Fatal("cancellation issued a grant")
	}
	session := strings.Repeat("s", 256)
	handle := "reason:" + strings.Repeat("x", 249)
	if _, err := e.PublishReasonGrants(context.Background(), session, []string{handle}, nil); err != nil {
		t.Fatal(err)
	}
	if e.gateGrants.bytes != 640 {
		t.Fatalf("accounted bytes=%d, want640", e.gateGrants.bytes)
	}
	if _, err := (*Engine)(nil).PublishReasonGrants(context.Background(), "s", []string{"reason:network"}, nil); !errors.Is(err, ErrGateGrantUnavailable) {
		t.Fatal("missing engine must refuse")
	}
}

func TestGateGrantsRawFactsNeverAuthorize(t *testing.T) {
	e, err := NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := e.AddFacts(context.Background(), []Fact{
		{Predicate: "disclosure_handle", Args: []interface{}{"s", "reason:network", "reason", now.UnixMilli()}, Timestamp: now},
		{Predicate: "js_gate_open", Args: []interface{}{"s", "low_confidence", now.UnixMilli()}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	if e.HasGateHandle("s", "reason:network") || e.HasGateReason("s", "low_confidence") {
		t.Fatal("raw facts minted authority")
	}
}

func TestGateGrantsConcurrentPublishLookup(t *testing.T) {
	e := grantTestEngine(gateGrantLimits{32, 256, 65536}, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := fmt.Sprintf("s-%d", i%8)
			for j := 0; j < 20; j++ {
				if _, err := e.PublishReasonGrants(context.Background(), session, []string{"reason:network"}, []string{"low_confidence"}); err != nil {
					t.Error(err)
					return
				}
				if !e.HasGateHandle(session, "reason:network") || !e.HasGateReason(session, "low_confidence") {
					t.Error("issued concurrent grant missing")
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if e.gateGrants.keys != 16 || len(e.gateGrants.sessions) != 8 || e.gateGrants.bytes > e.gateGrants.limits.bytes {
		t.Fatalf("concurrent accounting drift: %+v", e.gateGrants)
	}
}
