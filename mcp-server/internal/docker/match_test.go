package docker

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMatchRequestLinesPrefersPathThenNearbyErrors(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	logs := []LogEntry{
		{Container: "backend", Timestamp: at.Add(-20 * time.Second), Level: "INFO", Message: `"GET /api/fail HTTP/1.1" 500`},
		{Container: "backend", Timestamp: at.Add(40 * time.Millisecond), Level: "INFO", Message: `172.18.0.1 - "GET /api/fail HTTP/1.1" 500 Internal Server Error`},
		{Container: "backend", Timestamp: at.Add(30 * time.Millisecond), Level: "ERROR", Tag: "TRACEBACK", Message: "Traceback ...\nOperationalError: database unavailable"},
		{Container: "backend", Timestamp: at.Add(10 * time.Second), Level: "ERROR", Message: "unrelated error far away"},
		{Container: "backend", Timestamp: at.Add(50 * time.Millisecond), Level: "INFO", Message: `"GET /api/poll HTTP/1.1" 200`},
		{Container: "frontend", Timestamp: at.Add(2 * time.Minute), Level: "INFO", Message: `GET /api/fail outside the window`},
	}

	got := MatchRequestLines(logs, "/api/fail?x=1", at, 30*time.Second, 2*time.Second, 5)
	if len(got) != 3 {
		t.Fatalf("expected 3 matches (2 path, 1 nearby error), got %d: %+v", len(got), got)
	}
	if got[0].Reason != "path" || !strings.Contains(got[0].Entry.Message, "Internal Server Error") {
		t.Fatalf("nearest path line must come first, got %+v", got[0])
	}
	if got[1].Reason != "path" || got[1].Entry.Timestamp != at.Add(-20*time.Second) {
		t.Fatalf("second match must be the farther path line, got %+v", got[1])
	}
	if got[2].Reason != "time" || !strings.Contains(got[2].Entry.Message, "database unavailable") {
		t.Fatalf("third match must be the traceback beside the request, got %+v", got[2])
	}

	if capped := MatchRequestLines(logs, "/api/fail", at, 30*time.Second, 2*time.Second, 1); len(capped) != 1 {
		t.Fatalf("max must bound the result, got %d", len(capped))
	}
	if none := MatchRequestLines(logs, "/", at, 30*time.Second, 0, 5); len(none) != 0 {
		t.Fatalf("a root path matches nothing by path and errorWindow 0 matches nothing by time, got %+v", none)
	}
}

func TestMatchRequestLinesStripsAProxyPrefix(t *testing.T) {
	at := time.Now()
	logs := []LogEntry{{Container: "backend", Timestamp: at, Level: "INFO", Message: `"POST /v1/contacts/save HTTP/1.1" 422`}}
	got := MatchRequestLines(logs, "/api/v1/contacts/save", at, time.Second, 0, 5)
	if len(got) != 1 {
		t.Fatalf("the backend sees the path without the proxy's first segment, expected a match, got %+v", got)
	}
}

// TestLogsBetweenFailsFastWhenDockerIsUnreachable proves a dead daemon is an
// error, not an empty result, and costs well under a second.
func TestLogsBetweenFailsFastWhenDockerIsUnreachable(t *testing.T) {
	c := NewClient([]string{"cross-thread-backend", "cross-thread-frontend"}, 30*time.Second, "tcp://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	start := time.Now()
	logs, err := c.LogsBetween(ctx, time.Now().Add(-time.Minute), time.Now())
	elapsed := time.Since(start)
	t.Logf("unreachable docker answered in %s: %v", elapsed, err)
	if err == nil {
		t.Fatalf("an unreachable daemon must be an error, got %d lines", len(logs))
	}
	if elapsed > time.Second {
		t.Fatalf("an unreachable daemon took %s, want < 1s", elapsed)
	}
	if short := ShortError(err); short != "docker daemon not reachable" && short != "docker CLI not installed" {
		t.Fatalf("short error must name the unreachable daemon, got %q", short)
	}
}

func TestShortError(t *testing.T) {
	cases := map[string]error{
		"docker daemon not reachable":   errors.New("docker logs x: exit status 1: failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine"),
		"docker CLI not installed":      exec.ErrNotFound,
		"docker did not answer in time": context.DeadlineExceeded,
		"container not found":           errors.New("Error response from daemon: No such container: cross-thread-backend"),
	}
	for want, err := range cases {
		if got := ShortError(err); got != want {
			t.Errorf("ShortError(%v) = %q, want %q", err, got, want)
		}
	}
	if ShortError(nil) != "" {
		t.Error("nil error must be empty")
	}
}
