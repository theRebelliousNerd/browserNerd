package docker

import (
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
	client.query = func(_ context.Context, container string, _ time.Time) ([]LogEntry, error) {
		return nil, errors.New("docker logs " + container + ": error during connect: " + strings.Repeat("x", 2000))
	}
	logs, err := client.QueryLogs(context.Background(), time.Now().Add(-time.Minute))
	if err == nil {
		t.Fatalf("expected an error when every container fails, got %d logs and nil", len(logs))
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
	client.query = func(_ context.Context, container string, _ time.Time) ([]LogEntry, error) {
		if container == "frontend" {
			return nil, errors.New("docker logs frontend: No such container")
		}
		return []LogEntry{{Container: container, Level: "ERROR", Message: "boom"}}, nil
	}
	logs, err := client.QueryLogs(context.Background(), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("one answering container is a partial success, got %v", err)
	}
	if len(logs) != 1 || logs[0].Container != "backend" {
		t.Fatalf("expected the backend's log, got %+v", logs)
	}
}

func TestQueryLogsWithNoContainersIsEmpty(t *testing.T) {
	logs, err := NewClient(nil, 30*time.Second, "").QueryLogs(context.Background(), time.Now())
	if err != nil || len(logs) != 0 {
		t.Fatalf("no configured containers means nothing to query, got %d logs, %v", len(logs), err)
	}
}
