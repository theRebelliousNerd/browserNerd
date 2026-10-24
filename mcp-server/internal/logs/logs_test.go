package logs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFormatsAndMeasuredTime(t *testing.T) {
	local := time.FixedZone("fixture", -7*60*60)
	previous := time.Local
	time.Local = local
	t.Cleanup(func() { time.Local = previous })
	cases := []struct {
		text, level, message string
		stamp                time.Time
	}{
		{`{"timestamp":"2026-10-22T12:00:00+02:00","message":"GET /items failed","severity":"error"}`, "ERROR", "GET /items failed", time.Date(2026, 10, 22, 12, 0, 0, 0, time.FixedZone("", 7200))},
		{`{"time":"2026-10-22T12:00:00Z","msg":"request complete","level":"warn"}`, "WARNING", "request complete", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		{"2026-10-22 12:00:00.123 | ERROR | module:run - bad request", "ERROR", "module:run - bad request", time.Date(2026, 10, 22, 12, 0, 0, 123000000, local)},
		{"2026-10-22 12:00:00,123 WARNING: slow request", "WARNING", "slow request", time.Date(2026, 10, 22, 12, 0, 0, 123000000, local)},
		{"2026-10-22T12:00:00+0230 INFO: request complete", "INFO", "request complete", time.Date(2026, 10, 22, 12, 0, 0, 0, time.FixedZone("", 9000))},
		{"2026-10-22 12:00:00.123 +02:30 | ERROR | invalid item", "ERROR", "invalid item", time.Date(2026, 10, 22, 12, 0, 0, 123000000, time.FixedZone("", 9000))},
		{"INFO: no timestamp", "INFO", "no timestamp", time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			entries := Parse("api", tc.text)
			if len(entries) != 1 {
				t.Fatalf("want one entry, got %+v", entries)
			}
			e := entries[0]
			if e.Source != "api" || e.Level != tc.level || e.Message != tc.message || !e.Timestamp.Equal(tc.stamp) || e.Raw != tc.text {
				t.Fatalf("unexpected parsed entry: %+v", e)
			}
			_, gotOffset := e.Timestamp.Zone()
			_, wantOffset := tc.stamp.Zone()
			if gotOffset != wantOffset {
				t.Fatalf("offset lost: %d != %d", gotOffset, wantOffset)
			}
		})
	}
}

func TestParseTracebacksKeepRequestAndFirstTimestamp(t *testing.T) {
	for _, prefix := range []string{"", "2026-10-22T12:00:01Z "} {
		input := "2026-10-22T12:00:00Z ERROR: GET /items failed\n" + prefix + "Traceback (most recent call last):\n" + prefix + "  File \"/srv/app.py\", line 2\n" + prefix + "    result = handle()\n" + prefix + "ValueError: invalid item"
		entries := Parse("api", input)
		wantCount := 1
		if prefix != "" {
			wantCount = 2
		}
		if len(entries) != wantCount {
			t.Fatalf("want %d entries, got %+v", wantCount, entries)
		}
		trace := entries[len(entries)-1]
		if trace.Level != "ERROR" || trace.Tag != "TRACEBACK" || !strings.Contains(trace.Message, "result = handle()\nValueError") || !strings.Contains(trace.Raw, "File") {
			t.Fatalf("traceback fragmented: %+v", trace)
		}
		if prefix == "" && !strings.Contains(trace.Message, "GET /items") {
			t.Fatalf("request context lost: %+v", trace)
		}
	}
}

func TestParseDoesNotDateIndependentUndatedRecords(t *testing.T) {
	entries := Parse("api", "2026-10-22T12:00:00Z INFO: first\nINFO: independent undated record\n{\"message\":\"undated JSON\"}")
	if len(entries) != 3 || !entries[1].Timestamp.IsZero() || !entries[2].Timestamp.IsZero() {
		t.Fatalf("undated records inherited unrelated time: %+v", entries)
	}
}

func TestFilesWindowAppendAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	writeFile(t, path, "2026-10-22T11:59:00Z INFO: old\n2026-10-22T12:00:00Z ERROR: GET /items failed\n  File \"handler.py\", line 2\nValueError: invalid item\n")
	source := NewFiles([]File{{Name: "api", Path: path}}, 30*time.Second, 4096, 20)
	if source.LogWindow() != 30*time.Second {
		t.Fatal("configured window lost")
	}
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	entries, err := source.LogsBetween(context.Background(), at, at.Add(time.Minute))
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Message, "ValueError") {
		t.Fatalf("window or continuation failed: %+v, %v", entries, err)
	}
	handle, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = handle.WriteString("2026-10-22T12:00:01Z INFO: appended\n")
	handle.Close()
	if err != nil {
		t.Fatal(err)
	}
	entries, err = source.LogsBetween(context.Background(), at, at.Add(time.Minute))
	if err != nil || len(entries) != 2 || entries[1].Message != "appended" {
		t.Fatalf("append invisible: %+v, %v", entries, err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "2026-10-22T12:00:02Z INFO: rotated\n")
	entries, err = source.LogsBetween(context.Background(), at, at.Add(time.Minute))
	if err != nil || len(entries) != 1 || entries[0].Message != "rotated" {
		t.Fatalf("rotation invisible: %+v, %v", entries, err)
	}
}

func TestFilesExplicitPartialFailuresAndMissingTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.log")
	writeFile(t, path, "undated startup\n2026-10-22T12:00:00Z ERROR: GET /items failed\n")
	source := NewFiles([]File{{Name: "api", Path: path}, {Name: "worker", Path: filepath.Join(dir, "missing.log")}}, time.Second, 4096, 10)
	entries, err := source.LogsBetween(context.Background(), time.Time{}, time.Time{})
	if len(entries) != 1 || err == nil || !strings.Contains(err.Error(), MissingTimestamp) || !strings.Contains(err.Error(), "worker: unavailable") {
		t.Fatalf("partial failure must preserve usable evidence and both issues: %+v, %v", entries, err)
	}
	writeFile(t, path, "undated only\n")
	entries, err = source.LogsBetween(context.Background(), time.Time{}, time.Time{})
	if len(entries) != 0 || err == nil || !strings.Contains(err.Error(), MissingTimestamp) {
		t.Fatalf("undated file must be explicitly uncorrelatable: %+v, %v", entries, err)
	}
}

func TestFilesByteAndEntryBoundsAreVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.log")
	line := "2026-10-22T12:00:00Z ERROR: GET /items failed\n"
	writeFile(t, path, strings.Repeat(line, 100))
	for _, tc := range []struct {
		bytes int64
		cap   int
		want  int
	}{{int64(len(line)*3 + 5), 100, 3}, {100000, 2, 2}} {
		source := NewFiles([]File{{Name: "api", Path: path}}, time.Second, tc.bytes, tc.cap)
		entries, err := source.LogsBetween(context.Background(), time.Time{}, time.Time{})
		var issue *Issue
		if len(entries) != tc.want || !errors.As(err, &issue) || issue.Kind != Truncated {
			t.Fatalf("expected %d bounded entries + truncation, got %d, %v", tc.want, len(entries), err)
		}
	}
	writeFile(t, path, "2026-10-22T12:00:00Z ERROR: "+strings.Repeat("x", 100000)+"\n")
	entries, err := NewFiles([]File{{Name: "api", Path: path}}, time.Second, 200000, 2).LogsBetween(context.Background(), time.Time{}, time.Time{})
	if err != nil || len(entries) != 1 || len(entries[0].Message) != 100000 {
		t.Fatalf("long lines must not vanish at a scanner limit: %d, %v", len(entries), err)
	}
}

func TestFilesConfinementRecheckedAndNonregularRefused(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "external.log")
	writeFile(t, outside, "2026-10-22T12:00:00Z ERROR: outside\n")
	inside := filepath.Join(root, "inside.log")
	writeFile(t, inside, "2026-10-22T12:00:00Z INFO: inside\n")
	link := filepath.Join(root, "selected.log")
	if err := os.Symlink(inside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	source := NewFiles([]File{{Name: "api", Path: link, AllowedRoot: root}}, time.Second, 4096, 10)
	entries, err := source.LogsBetween(context.Background(), time.Time{}, time.Time{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("inside symlink must work: %+v, %v", entries, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	entries, err = source.LogsBetween(context.Background(), time.Time{}, time.Time{})
	if len(entries) != 0 || err == nil || !strings.Contains(err.Error(), "outside allowed root") {
		t.Fatalf("retargeted symlink escaped: %+v, %v", entries, err)
	}
	entries, err = NewFiles([]File{{Name: "external", Path: outside}}, time.Second, 4096, 10).LogsBetween(context.Background(), time.Time{}, time.Time{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("explicit external source refused: %+v, %v", entries, err)
	}
	entries, err = NewFiles([]File{{Name: "directory", Path: root}}, time.Second, 4096, 10).LogsBetween(context.Background(), time.Time{}, time.Time{})
	if len(entries) != 0 || err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("nonregular source accepted: %+v, %v", entries, err)
	}
}

func TestFilesCancellationAndInvalidLimits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	entries, err := NewFiles([]File{{Name: "api", Path: "missing.log"}}, time.Second, 4096, 10).LogsBetween(ctx, time.Time{}, time.Time{})
	if len(entries) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %+v, %v", entries, err)
	}
	for _, source := range []*Files{NewFiles(nil, time.Second, 1, 1), NewFiles(nil, time.Second, 0, 1), NewFiles(nil, time.Second, 1, 0)} {
		if _, err := source.LogsBetween(context.Background(), time.Time{}, time.Time{}); err == nil {
			t.Fatal("invalid source must not report empty success")
		}
	}
}

type cancelOnRead struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelOnRead) Read(data []byte) (int, error) {
	r.reads++
	r.cancel()
	return copy(data, "partial evidence"), nil
}

func TestCancellationStopsAnActiveReadAndParsing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelOnRead{cancel: cancel}
	data, err := io.ReadAll(&contextReader{ctx: ctx, reader: reader})
	if !errors.Is(err, context.Canceled) || reader.reads != 1 || string(data) != "partial evidence" {
		t.Fatalf("read continued after cancellation: reads=%d data=%q err=%v", reader.reads, data, err)
	}
	if entries, err := ParseContext(ctx, "api", strings.Repeat("2026-10-22T12:00:00Z INFO: item\n", 10000)); !errors.Is(err, context.Canceled) || len(entries) != 0 {
		t.Fatalf("parser ignored cancellation: entries=%d err=%v", len(entries), err)
	}
}

func TestFilesSlowSiblingCannotSuppressHealthyEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy.log")
	writeFile(t, path, "2026-10-22T12:00:00Z ERROR: GET /items failed\n")
	source := NewFiles([]File{{Name: "slow"}, {Name: "healthy", Path: path}, {Name: "later", Path: path}}, time.Second, 4096, 10)
	readFile := source.readFile
	source.readFile = func(ctx context.Context, file File, since, until time.Time) ([]Entry, error) {
		if file.Name == "slow" {
			<-ctx.Done()
			return nil, &Issue{Source: file.Name, Kind: Unavailable, Err: ctx.Err()}
		}
		return readFile(ctx, file, since, until)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	entries, err := source.LogsBetween(ctx, time.Time{}, time.Time{})
	if len(entries) != 2 || entries[0].Source != "healthy" || entries[1].Source != "later" || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "slow: unavailable") {
		t.Fatalf("slow file suppressed healthy evidence or source order: %+v, %v", entries, err)
	}
}

type fixtureSource struct {
	entries []Entry
	err     error
}

func (f fixtureSource) LogWindow() time.Duration { return time.Second }
func (f fixtureSource) LogsBetween(context.Context, time.Time, time.Time) ([]Entry, error) {
	return f.entries, f.err
}

func TestCompositePreservesPartialEvidenceAndAllFailures(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	issue := &Issue{Source: "unreachable", Kind: Unavailable, Err: errors.New("not running")}
	sources := []Source{fixtureSource{entries: []Entry{{Source: "api", Timestamp: at}}}, fixtureSource{entries: []Entry{{Source: "worker", Timestamp: at.Add(-time.Second)}}, err: &Issue{Source: "worker", Kind: Truncated}}, fixtureSource{err: issue}}
	source := NewComposite(sources, 30*time.Second)
	entries, err := source.LogsBetween(context.Background(), time.Time{}, time.Time{})
	if source.LogWindow() != 30*time.Second || len(entries) != 2 || entries[0].Source != "worker" || !errors.Is(err, issue) || !strings.Contains(err.Error(), Truncated) {
		t.Fatalf("partial evidence or issues lost: %+v, %v", entries, err)
	}
	for _, sources := range [][]Source{nil, {fixtureSource{err: issue}}} {
		entries, err := NewComposite(sources, time.Second).LogsBetween(context.Background(), time.Time{}, time.Time{})
		if len(entries) != 0 || err == nil {
			t.Fatalf("unavailable must not be empty success: %+v, %v", entries, err)
		}
	}
}

func TestMatcherRanksPathsAndNeverDatesMissingRecords(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Source: "api", Timestamp: at.Add(-time.Second), Level: "INFO", Message: "GET /v1/items failed"},
		{Source: "api", Timestamp: at.Add(10 * time.Millisecond), Level: "ERROR", Message: "ValueError: invalid item"},
		{Source: "api", Timestamp: at.Add(time.Minute), Level: "ERROR", Message: "GET /v1/items far away"},
		{Source: "api", Level: "ERROR", Message: "GET /v1/items without time"},
	}
	got := MatchRequestLines(entries, "/proxy/v1/items?limit=2", at, 3*time.Second, time.Second, 2)
	if len(got) != 2 || got[0].Reason != "path" || got[1].Reason != "time" {
		t.Fatalf("bad ranking: %+v", got)
	}
	if len(MatchRequestLines(entries, "/", at, time.Second, 0, 3)) != 0 || len(MatchRequestLines(entries, "/v1/items", at, time.Second, time.Second, 1)) != 1 {
		t.Fatal("path/cap contract violated")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
