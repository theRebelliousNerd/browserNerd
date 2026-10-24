package docker

import (
	"browsernerd-mcp-server/internal/logs"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// With Docker Desktop stopped every container fails; that must surface as an
// error, not as a successful query that found no logs.
func TestQueryLogsFailsWhenEveryContainerFails(t *testing.T) {
	client := NewClient([]string{"backend", "frontend"}, 30*time.Second, "")
	client.query = func(_ context.Context, container string, _ time.Time) ([]logs.Entry, error) {
		return nil, errors.New("docker logs " + container + ": error during connect: " + strings.Repeat("x", 2000))
	}
	entries, err := client.QueryLogs(context.Background(), time.Now().Add(-time.Minute))
	if err == nil {
		t.Fatalf("expected an error when every container fails, got %d logs and nil", len(entries))
	}
	if !strings.Contains(err.Error(), "every container (2)") || !strings.Contains(err.Error(), "backend") || !strings.Contains(err.Error(), "frontend") {
		t.Fatalf("error must name the failure and each container: %v", err)
	}
	if len(err.Error()) > 1000 {
		t.Fatalf("error text must stay bounded, got %d bytes", len(err.Error()))
	}
}

func TestQueryLogsKeepsPartialSuccess(t *testing.T) {
	client := NewClient([]string{"backend", "frontend"}, 30*time.Second, "")
	client.query = func(_ context.Context, container string, _ time.Time) ([]logs.Entry, error) {
		if container == "frontend" {
			return nil, errors.New("docker logs frontend: No such container")
		}
		return []logs.Entry{{Source: container, Level: "ERROR", Message: "boom"}}, nil
	}
	entries, err := client.QueryLogs(context.Background(), time.Now().Add(-time.Minute))
	if err == nil || !strings.Contains(err.Error(), "frontend") {
		t.Fatalf("partial failure must remain visible alongside healthy evidence: %v", err)
	}
	if len(entries) != 1 || entries[0].Source != "backend" {
		t.Fatalf("expected the backend's log, got %+v", entries)
	}
}

func TestQueryLogsWithNoContainersIsEmpty(t *testing.T) {
	entries, err := NewClient(nil, 30*time.Second, "").QueryLogs(context.Background(), time.Now())
	if err != nil || len(entries) != 0 {
		t.Fatalf("no configured containers means nothing to query, got %d logs, %v", len(entries), err)
	}
}
