package browser

import (
	"sync"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// longLivedRequestAge is how long a request may stay in flight before it is
// treated as a connection rather than a load: a long poll or a streaming
// response never finishes, and must not keep a page from ever settling.
const longLivedRequestAge = 60 * time.Second

// networkActivity counts one tab's requests in flight from the moment the
// tab's event stream starts, so a caller that begins watching after a fetch
// was sent still sees that fetch (docs/specs/responsive-layout-audit.md,
// "Readiness").
type networkActivity struct {
	mu       sync.Mutex
	inflight map[proto.NetworkRequestID]time.Time
	last     time.Time
}

func newNetworkActivity() *networkActivity {
	return &networkActivity{inflight: make(map[proto.NetworkRequestID]time.Time)}
}

// started records a request leaving the tab. A redirect re-sends the same
// id and keeps one entry. WebSockets and EventSource streams stay open by
// design and are not load.
func (n *networkActivity) started(id proto.NetworkRequestID, resourceType proto.NetworkResourceType, at time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last = at
	if resourceType == proto.NetworkResourceTypeWebSocket || resourceType == proto.NetworkResourceTypeEventSource {
		return
	}
	if _, exists := n.inflight[id]; !exists && len(n.inflight) >= maxTrackedRequests {
		n.pruneLocked(at)
		if len(n.inflight) >= maxTrackedRequests {
			return
		}
	}
	n.inflight[id] = at
}

// ended records a request finishing or failing.
func (n *networkActivity) ended(id proto.NetworkRequestID, at time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.inflight[id]; ok {
		n.last = at
		delete(n.inflight, id)
	}
}

// pruneLocked drops entries in flight longer than longLivedRequestAge.
func (n *networkActivity) pruneLocked(now time.Time) {
	for id, sent := range n.inflight {
		if now.Sub(sent) > longLivedRequestAge {
			delete(n.inflight, id)
		}
	}
}

// NetworkState is a tab's network activity at one instant.
type NetworkState struct {
	// Tracked is false when the session has no event stream (Mangle
	// disabled), so nothing below was observed.
	Tracked bool
	// InFlight counts requests sent and not yet finished or failed, leaving
	// out WebSockets, EventSource streams and requests older than 60 s.
	InFlight int
	// LastActivity is when a request last started or ended.
	LastActivity time.Time
}

// Quiet reports whether no request is in flight and none started or ended
// within window of now. An untracked tab is never reported quiet.
func (s NetworkState) Quiet(now time.Time, window time.Duration) bool {
	return s.Tracked && s.InFlight == 0 && now.Sub(s.LastActivity) >= window
}

func (n *networkActivity) state(now time.Time) NetworkState {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, sent := range n.inflight {
		if now.Sub(sent) <= longLivedRequestAge {
			count++
		}
	}
	return NetworkState{Tracked: true, InFlight: count, LastActivity: n.last}
}

// NetworkState reports the session's requests in flight and when the network
// last moved.
func (m *SessionManager) NetworkState(sessionID string) NetworkState {
	m.mu.RLock()
	rec, ok := m.sessions[sessionID]
	var net *networkActivity
	if ok && rec != nil {
		net = rec.net
	}
	m.mu.RUnlock()
	if net == nil {
		return NetworkState{}
	}
	return net.state(time.Now())
}
