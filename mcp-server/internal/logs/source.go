// Package logs collects and correlates timestamped server evidence independently
// of the transport used to retrieve it.
package logs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Entry struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Tag       string    `json:"tag"`
	Message   string    `json:"message"`
	Raw       string    `json:"raw"`
}

type Source interface {
	LogsBetween(context.Context, time.Time, time.Time) ([]Entry, error)
	LogWindow() time.Duration
}

// Issue describes an evidence limitation. Entries returned alongside an issue
// remain usable; callers must also surface the issue to their reader.
type Issue struct {
	Source string
	Kind   string
	Err    error
}

const (
	Unavailable      = "unavailable"
	MissingTimestamp = "missing_timestamp"
	Truncated        = "truncated"
)

func (i *Issue) Error() string {
	if i.Err == nil {
		return fmt.Sprintf("log source %s: %s", i.Source, i.Kind)
	}
	return fmt.Sprintf("log source %s: %s: %v", i.Source, i.Kind, i.Err)
}

func (i *Issue) Unwrap() error { return i.Err }

type composite struct {
	sources []Source
	window  time.Duration
}

func NewComposite(sources []Source, window time.Duration) Source {
	return &composite{sources: append([]Source(nil), sources...), window: window}
}

func (c *composite) LogWindow() time.Duration { return c.window }

func (c *composite) LogsBetween(ctx context.Context, since, until time.Time) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(c.sources) == 0 {
		return nil, &Issue{Kind: Unavailable, Err: errors.New("no log sources configured")}
	}
	type result struct {
		entries []Entry
		err     error
	}
	results := make([]result, len(c.sources))
	var wg sync.WaitGroup
	for i, source := range c.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if source == nil {
				results[i].err = &Issue{Kind: Unavailable, Err: errors.New("nil log source")}
				return
			}
			results[i].entries, results[i].err = source.LogsBetween(ctx, since, until)
		}()
	}
	wg.Wait()
	var all []Entry
	var issues []error
	for _, result := range results {
		all = append(all, result.entries...)
		if result.err != nil {
			issues = append(issues, result.err)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Timestamp.Before(all[j].Timestamp) })
	return all, errors.Join(issues...)
}
