package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/logs"
)

func helperDocker(t *testing.T, mode string) *Client {
	t.Helper()
	client := NewClient([]string{"worker"}, time.Minute, "")
	client.command = func(ctx context.Context, args ...string) *exec.Cmd {
		if !strings.Contains(strings.Join(args, " "), "--tail 2001") {
			t.Errorf("Docker output must be bounded at the source: %v", args)
		}
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestDockerLogCommandHelper", "--", mode)
		// The race runtime's exit delay is not Docker work and exceeds the
		// transport's real deadline. Keep race checks enabled without that delay.
		command.Env = append(os.Environ(), "BROWSERNERD_DOCKER_HELPER=1", "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
		return command
	}
	return client
}

func TestDockerLogCommandHelper(t *testing.T) {
	if os.Getenv("BROWSERNERD_DOCKER_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	stamp := time.Now().Format(time.RFC3339Nano)
	switch mode {
	case "overflow":
		fmt.Printf("%s ERROR GET /jobs JobError: retained evidence\n", stamp)
		for i := 0; i < 2048; i++ {
			fmt.Println(strings.Repeat("x", 2048))
		}
	case "entries":
		for i := 0; i < maxDockerEntries+1; i++ {
			fmt.Printf("%s ERROR GET /jobs JobError: %d\n", stamp, i)
		}
	case "hang":
		time.Sleep(30 * time.Second)
	case "undated":
		fmt.Println("ERROR GET /jobs JobError: no timestamp")
	}
	os.Exit(0)
}

func TestDockerBoundedOutputRetainsEvidenceAndTruncation(t *testing.T) {
	client := helperDocker(t, "overflow")
	entries, err := client.LogsBetween(context.Background(), time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
	var issue *logs.Issue
	if len(entries) == 0 || !strings.Contains(entries[0].Message, "retained evidence") || !errors.As(err, &issue) || issue.Kind != logs.Truncated {
		t.Fatalf("bounded Docker read discarded evidence or cap issue: entries=%d err=%v", len(entries), err)
	}
	for _, entry := range entries {
		if len(entry.Raw) > maxDockerBytes {
			t.Fatal("transport retained more than its byte budget")
		}
	}
}

func TestDockerEntryCapRetainsNewestEvidence(t *testing.T) {
	client := helperDocker(t, "entries")
	entries, err := client.QueryLogs(context.Background(), time.Now().Add(-time.Minute))
	var issue *logs.Issue
	if len(entries) != maxDockerEntries || !errors.As(err, &issue) || issue.Kind != logs.Truncated {
		t.Fatalf("entry cap must retain typed issue and bounded records: entries=%d err=%v", len(entries), err)
	}
	if !strings.Contains(entries[len(entries)-1].Message, fmt.Sprint(maxDockerEntries)) {
		t.Fatal("entry cap must retain the newest records")
	}
}

func TestDockerHungCommandHasInternalDeadline(t *testing.T) {
	client := helperDocker(t, "hang")
	start := time.Now()
	_, err := client.LogsBetween(context.Background(), start.Add(-time.Minute), start)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("Docker query escaped its internal deadline: elapsed=%s err=%v", time.Since(start), err)
	}
}

func TestDockerCommandHonorsCallerCancellation(t *testing.T) {
	client := helperDocker(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := client.QueryLogs(ctx, start)
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("cancelled caller was ignored: %v", err)
	}
}

func TestDockerUndatedRecordsAreUnavailableForCorrelation(t *testing.T) {
	client := helperDocker(t, "undated")
	entries, err := client.LogsBetween(context.Background(), time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
	var issue *logs.Issue
	if len(entries) != 0 || !errors.As(err, &issue) || issue.Kind != logs.MissingTimestamp {
		t.Fatalf("undated Docker output became fabricated evidence: %v %v", entries, err)
	}
}
