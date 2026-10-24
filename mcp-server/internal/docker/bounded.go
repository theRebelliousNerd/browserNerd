package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/logs"
)

const (
	maxDockerBytes   = 1 << 20
	maxDockerEntries = 2000
	dockerDeadline   = 900 * time.Millisecond
)

type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(len(data), b.limit-len(b.data))
	b.data = append(b.data, data[:n]...)
	if n != len(data) {
		b.overflow = true
		b.cancel()
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (c *Client) queryContainerBetween(ctx context.Context, container string, since, until time.Time) ([]logs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, &logs.Issue{Source: container, Kind: logs.Unavailable, Err: err}
	}
	operationCtx, finish := context.WithTimeout(ctx, dockerDeadline)
	defer finish()
	qctx, cancel := context.WithCancel(operationCtx)
	defer cancel()
	// One extra physical line lets the reader disclose a possible tail cap.
	args := []string{"logs", "--timestamps", "--tail", strconv.Itoa(maxDockerEntries + 1), "--since", since.UTC().Format(time.RFC3339Nano)}
	if !until.IsZero() {
		args = append(args, "--until", until.UTC().Format(time.RFC3339Nano))
	}
	if c.host != "" {
		args = append([]string{"-H", c.host}, args...)
	}
	args = append(args, container)
	output := &boundedOutput{limit: maxDockerBytes, cancel: cancel}
	cmd := c.command(qctx, args...)
	cmd.Stdout, cmd.Stderr = output, output
	// A child retaining an inherited pipe cannot extend the query indefinitely.
	cmd.WaitDelay = 100 * time.Millisecond
	runErr := cmd.Run()
	var issues []error
	issue := func(kind string, err error) {
		issues = append(issues, &logs.Issue{Source: container, Kind: kind, Err: err})
	}
	data := output.data
	if output.overflow {
		issue(logs.Truncated, fmt.Errorf("Docker output exceeded %d bytes", maxDockerBytes))
		// A capped physical line cannot be represented as complete evidence.
		if last := bytes.LastIndexByte(data, '\n'); last >= 0 {
			data = data[:last+1]
		} else {
			data = nil
		}
	} else if runErr != nil {
		if err := qctx.Err(); err != nil {
			issue(logs.Unavailable, fmt.Errorf("docker logs %s: %w", container, err))
		} else {
			issue(logs.Unavailable, fmt.Errorf("docker logs %s: %w: %s", container, runErr, boundText(string(data), maxContainerErrorBytes)))
		}
	}
	if bytes.Count(data, []byte{'\n'}) >= maxDockerEntries+1 {
		issue(logs.Truncated, fmt.Errorf("Docker tail may omit entries before the newest %d physical lines", maxDockerEntries+1))
	}
	// Overflow cancels only the CLI query; the caller's context still governs
	// parsing the bounded evidence obtained before that cap.
	entries, parseErr := logs.ParseContext(operationCtx, container, string(data))
	if parseErr != nil {
		issue(logs.Unavailable, parseErr)
	}
	filtered := make([]logs.Entry, 0, min(len(entries), maxDockerEntries))
	undated := 0
	for _, entry := range entries {
		if entry.Timestamp.IsZero() {
			undated++
			continue
		}
		if entry.Timestamp.Before(since) || (!until.IsZero() && entry.Timestamp.After(until)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	if undated > 0 && (runErr == nil || output.overflow) {
		issue(logs.MissingTimestamp, fmt.Errorf("%d entries have no measured timestamp", undated))
	}
	if len(filtered) > maxDockerEntries {
		filtered = filtered[len(filtered)-maxDockerEntries:]
		issue(logs.Truncated, fmt.Errorf("Docker evidence exceeded %d entries", maxDockerEntries))
	}

	return filtered, errors.Join(issues...)
}
