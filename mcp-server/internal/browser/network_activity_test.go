package browser

import (
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

func TestNetworkActivityCountsLoadNotConnections(t *testing.T) {
	n := newNetworkActivity()
	t0 := time.Now()
	n.started("doc", proto.NetworkResourceTypeDocument, t0)
	n.started("xhr", proto.NetworkResourceTypeFetch, t0)
	n.started("xhr", proto.NetworkResourceTypeFetch, t0) // a redirect re-sends the id
	n.started("ws", proto.NetworkResourceTypeWebSocket, t0)
	n.started("sse", proto.NetworkResourceTypeEventSource, t0)
	if got := n.state(t0).InFlight; got != 2 {
		t.Fatalf("in flight = %d, want 2 (document and fetch; sockets and streams are not load)", got)
	}
	n.ended("doc", t0.Add(100*time.Millisecond))
	n.ended("unknown", t0.Add(900*time.Millisecond)) // never tracked: does not move the clock
	s := n.state(t0.Add(time.Second))
	if s.InFlight != 1 || !s.LastActivity.Equal(t0.Add(100*time.Millisecond)) {
		t.Fatalf("state = %+v, want one in flight, last activity at the document's end", s)
	}
	if s.Quiet(t0.Add(time.Second), 500*time.Millisecond) {
		t.Fatalf("quiet with a fetch in flight")
	}
	// A request in flight past longLivedRequestAge is a connection, not load.
	late := t0.Add(longLivedRequestAge + time.Second)
	if s := n.state(late); s.InFlight != 0 || !s.Quiet(late, 500*time.Millisecond) {
		t.Fatalf("a long poll still counts: %+v", s)
	}
	n.ended("xhr", t0.Add(2*time.Second))
	s = n.state(t0.Add(2*time.Second + 200*time.Millisecond))
	if s.InFlight != 0 || s.Quiet(t0.Add(2*time.Second+200*time.Millisecond), 500*time.Millisecond) {
		t.Fatalf("quiet 200 ms after the last request ended: %+v", s)
	}
	if !s.Quiet(t0.Add(2*time.Second+600*time.Millisecond), 500*time.Millisecond) {
		t.Fatalf("not quiet 600 ms after the last request ended")
	}
	if (NetworkState{}).Quiet(late, 0) {
		t.Fatalf("an untracked tab reported quiet")
	}
}
