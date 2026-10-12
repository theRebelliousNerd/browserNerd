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
	inflight map[proto.NetworkRequestID]pendingRequest
	last     time.Time
}

// pendingRequest is a request in flight: when it was sent, and the frame and
// document (loader) that sent it.
type pendingRequest struct {
	sent   time.Time
	frame  proto.PageFrameID
	loader proto.NetworkLoaderID
}

func newNetworkActivity() *networkActivity {
	return &networkActivity{inflight: make(map[proto.NetworkRequestID]pendingRequest)}
}

// started records a request leaving the tab. A redirect re-sends the same
// id and keeps one entry. WebSockets and EventSource streams stay open by
// design and are not load. A document request starts a new document in its
// frame: requests the frame's previous document still had in flight are
// dropped, because Chrome does not always report the ones a navigation
// cancels, and they would otherwise read as load for a minute.
func (n *networkActivity) started(ev *proto.NetworkRequestWillBeSent, at time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last = at
	switch ev.Type {
	case proto.NetworkResourceTypeWebSocket, proto.NetworkResourceTypeEventSource:
		return
	case proto.NetworkResourceTypeDocument:
		for id, req := range n.inflight {
			if req.frame == ev.FrameID && req.loader != ev.LoaderID {
				delete(n.inflight, id)
			}
		}
	}
	if _, exists := n.inflight[ev.RequestID]; !exists && len(n.inflight) >= maxTrackedRequests {
		n.pruneLocked(at)
		if len(n.inflight) >= maxTrackedRequests {
			return
		}
	}
	n.inflight[ev.RequestID] = pendingRequest{sent: at, frame: ev.FrameID, loader: ev.LoaderID}
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
	for id, req := range n.inflight {
		if now.Sub(req.sent) > longLivedRequestAge {
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
	for _, req := range n.inflight {
		if now.Sub(req.sent) <= longLivedRequestAge {
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
