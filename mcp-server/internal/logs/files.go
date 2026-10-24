package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type File struct {
	Name        string
	Path        string
	AllowedRoot string
}

type Files struct {
	files        []File
	window       time.Duration
	maxReadBytes int64
	maxEntries   int
	readFile     func(context.Context, File, time.Time, time.Time) ([]Entry, error)
}

func NewFiles(files []File, window time.Duration, maxReadBytes int64, maxEntries int) *Files {
	source := &Files{files: append([]File(nil), files...), window: window, maxReadBytes: maxReadBytes, maxEntries: maxEntries}
	source.readFile = source.read
	return source
}

func (f *Files) LogWindow() time.Duration { return f.window }

func (f *Files) LogsBetween(ctx context.Context, since, until time.Time) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.maxReadBytes <= 0 || f.maxEntries <= 0 || f.window <= 0 {
		return nil, &Issue{Kind: Unavailable, Err: errors.New("log read limits and window must be positive")}
	}
	if len(f.files) == 0 {
		return nil, &Issue{Kind: Unavailable, Err: errors.New("no log files configured")}
	}
	type result struct {
		entries []Entry
		err     error
	}
	results := make([]result, len(f.files))
	var wg sync.WaitGroup
	for i, file := range f.files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i].entries, results[i].err = f.readFile(ctx, file, since, until)
		}()
	}
	wg.Wait()
	var entries []Entry
	var issues []error
	// Collection is concurrent, while output follows configured source order.
	for _, result := range results {
		entries = append(entries, result.entries...)
		if result.err != nil {
			issues = append(issues, result.err)
		}
	}
	return entries, errors.Join(issues...)
}

func (f *Files) read(ctx context.Context, file File, since, until time.Time) ([]Entry, error) {
	handle, err := openRegular(file)
	if err != nil {
		return nil, &Issue{Source: file.Name, Kind: Unavailable, Err: err}
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, &Issue{Source: file.Name, Kind: Unavailable, Err: err}
	}
	start := int64(0)
	var issues []error
	if info.Size() > f.maxReadBytes {
		start = info.Size() - f.maxReadBytes
		issues = append(issues, &Issue{Source: file.Name, Kind: Truncated, Err: fmt.Errorf("file tail limited to %d bytes", f.maxReadBytes)})
	}
	reader := &contextReader{ctx, io.NewSectionReader(handle, start, info.Size()-start)}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.Join(append(issues, &Issue{Source: file.Name, Kind: Unavailable, Err: err})...)
	}
	if start > 0 {
		// The leading line may have begun before the tail. Never interpret a
		// fragment as a complete record, even if it resembles a timestamp.
		if boundary := strings.IndexByte(string(data), '\n'); boundary >= 0 {
			data = data[boundary+1:]
		} else {
			data = nil
		}
	}
	parsed, parseErr := ParseContext(ctx, file.Name, string(data))
	if parseErr != nil {
		issues = append(issues, &Issue{Source: file.Name, Kind: Unavailable, Err: parseErr})
	}
	var entries []Entry
	missing := false
	for _, entry := range parsed {
		if err := ctx.Err(); err != nil {
			return entries, errors.Join(append(issues, &Issue{Source: file.Name, Kind: Unavailable, Err: err})...)
		}
		if entry.Timestamp.IsZero() {
			missing = true
			continue
		}
		if (!since.IsZero() && entry.Timestamp.Before(since)) || (!until.IsZero() && entry.Timestamp.After(until)) {
			continue
		}
		entries = append(entries, entry)
	}
	if missing {
		issues = append(issues, &Issue{Source: file.Name, Kind: MissingTimestamp, Err: errors.New("undated records cannot be correlated")})
	}
	if len(entries) > f.maxEntries {
		entries = entries[len(entries)-f.maxEntries:]
		issues = append(issues, &Issue{Source: file.Name, Kind: Truncated, Err: fmt.Errorf("entries limited to %d", f.maxEntries)})
	}
	return entries, errors.Join(issues...)
}

// openRegular checks each new query against current symlink targets. os.Root
// also prevents a symlink swap during open from escaping the allowed root.
func openRegular(file File) (*os.File, error) {
	resolved, err := filepath.EvalSymlinks(file.Path)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	rootPath := filepath.Dir(resolved)
	if file.AllowedRoot != "" {
		rootPath, err = filepath.EvalSymlinks(file.AllowedRoot)
		if err != nil {
			return nil, err
		}
		rootPath, err = filepath.Abs(rootPath)
		if err != nil {
			return nil, err
		}
	}
	relative, err := filepath.Rel(rootPath, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("log file resolves outside allowed root")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat(relative)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("log source is not a regular file")
	}
	handle, err := root.OpenFile(relative, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, err
	}
	opened, err := handle.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		handle.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("log source is not a regular file")
	}
	return handle, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
