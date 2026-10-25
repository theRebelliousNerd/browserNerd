package mangle

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	GateGrantTTL         = 10 * time.Minute
	GateGrantMaxSessions = 256
	GateGrantMaxKeys     = 4096
	GateGrantMaxBytes    = 1 << 20
)

var (
	ErrGateGrantCapacity    = errors.New("authorization_capacity_exhausted")
	ErrGateGrantInvalid     = errors.New("invalid_authorization_bundle")
	ErrGateGrantEmpty       = errors.New("no_eligible_grants")
	ErrGateGrantUnavailable = errors.New("authorization_unavailable")
)

type gateGrantKey struct {
	kind  byte
	value string
}

type gateGrantLimits struct{ sessions, keys, bytes int }

// gateGrantLedger is control state, never populated by observation ingestion.
// Accounted bytes include 64 per session/key in addition to their string bytes;
// the independent count and string limits also bound runtime map overhead.
type gateGrantLedger struct {
	mu       sync.Mutex
	sessions map[string]map[gateGrantKey]time.Time
	keys     int
	bytes    int
	limits   gateGrantLimits
	clock    func() time.Time
}

func newGateGrantLedger(limits gateGrantLimits) *gateGrantLedger {
	return &gateGrantLedger{sessions: make(map[string]map[gateGrantKey]time.Time), limits: limits, clock: time.Now}
}

func grantStringValid(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func grantReasonValid(value string) bool {
	return value == "low_confidence" || value == "contradiction_detected" || value == "no_matching_tool"
}

func (g *gateGrantLedger) expireLocked(now time.Time) {
	for session, grants := range g.sessions {
		for key, expiry := range grants {
			if !expiry.After(now) {
				delete(grants, key)
				g.keys--
				g.bytes -= len(key.value) + 64
			}
		}
		if len(grants) == 0 {
			delete(g.sessions, session)
			g.bytes -= len(session) + 64
		}
	}
}

// PublishReasonGrants is the typed publication port for browser-reason alone.
// Facts, including identically named raw telemetry, never call this port.
// The whole deduplicated bundle is admitted or refused before any live refresh.
func (e *Engine) PublishReasonGrants(ctx context.Context, session string, handles, reasons []string) (time.Time, error) {
	if e == nil || !e.cfg.Enable || e.gateGrants == nil {
		return time.Time{}, ErrGateGrantUnavailable
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if !grantStringValid(session) || len(handles)+len(reasons) > GateGrantMaxKeys {
		return time.Time{}, ErrGateGrantInvalid
	}
	keys := make(map[gateGrantKey]struct{}, len(handles)+len(reasons))
	for _, handle := range handles {
		if !grantStringValid(handle) || !strings.HasPrefix(handle, "reason:") || len(handle) == len("reason:") {
			return time.Time{}, ErrGateGrantInvalid
		}
		keys[gateGrantKey{kind: 'h', value: handle}] = struct{}{}
	}
	for _, reason := range reasons {
		if !grantReasonValid(reason) {
			return time.Time{}, ErrGateGrantInvalid
		}
		keys[gateGrantKey{kind: 'r', value: reason}] = struct{}{}
	}
	if len(keys) == 0 {
		return time.Time{}, ErrGateGrantEmpty
	}
	g := e.gateGrants
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	g.expireLocked(now)
	current := g.sessions[session]
	newSession, newKeys, newBytes := 0, 0, 0
	if current == nil {
		newSession = 1
		newBytes = len(session) + 64
	}
	for key := range keys {
		if _, exists := current[key]; !exists {
			newKeys++
			newBytes += len(key.value) + 64
		}
	}
	if len(g.sessions)+newSession > g.limits.sessions || g.keys+newKeys > g.limits.keys || g.bytes+newBytes > g.limits.bytes {
		return time.Time{}, ErrGateGrantCapacity
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	expiry := now.Add(GateGrantTTL)
	if current == nil {
		current = make(map[gateGrantKey]time.Time, len(keys))
		g.sessions[session] = current
	}
	for key := range keys {
		current[key] = expiry
	}
	g.keys += newKeys
	g.bytes += newBytes
	return expiry, nil
}

func (e *Engine) hasGateGrant(session string, key gateGrantKey) bool {
	if e == nil || !e.cfg.Enable || e.gateGrants == nil || !grantStringValid(session) || !grantStringValid(key.value) {
		return false
	}
	g := e.gateGrants
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	g.expireLocked(now)
	return g.sessions[session][key].After(now)
}

func (e *Engine) HasGateHandle(session, handle string) bool {
	return e.hasGateGrant(session, gateGrantKey{kind: 'h', value: handle})
}

func (e *Engine) HasGateReason(session, reason string) bool {
	return grantReasonValid(reason) && e.hasGateGrant(session, gateGrantKey{kind: 'r', value: reason})
}
