package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/security"
)

const (
	MaxRotatedFiles = 3
	TraceDir        = "data/traces"
)

// Event represents a single record in the flight recorder.
type Event struct {
	Timestamp time.Time   `json:"ts"`
	Type      string      `json:"type"`
	SessionID string      `json:"session_id,omitempty"`
	Data      interface{} `json:"data"`
}

// Recorder manages rotating logs for session debugging.
type Recorder struct {
	mu              sync.Mutex
	file            *os.File
	encoder         *json.Encoder
	basePath        string
	maxRotatedFiles int
	currentPath     string
	redactor        *security.Redactor
}

// NewRecorder creates a recorder instance.
// It ensures the directory exists.
func NewRecorder(basePath string) (*Recorder, error) {
	return NewRecorderWithOptions(basePath, MaxRotatedFiles)
}

// NewRecorderWithOptions creates a recorder with explicit rotation settings.
func NewRecorderWithOptions(basePath string, maxRotatedFiles int) (*Recorder, error) {
	return NewRecorderWithSecurity(basePath, maxRotatedFiles, security.NewRedactor(nil))
}

// NewRecorderWithSecurity creates a recorder with explicit rotation and redaction.
func NewRecorderWithSecurity(basePath string, maxRotatedFiles int, redactor *security.Redactor) (*Recorder, error) {
	if basePath == "" {
		basePath = TraceDir
	}
	if maxRotatedFiles <= 0 {
		maxRotatedFiles = MaxRotatedFiles
	}
	if err := security.EnsurePrivateDir(basePath); err != nil {
		return nil, err
	}
	return &Recorder{
		basePath:        basePath,
		maxRotatedFiles: maxRotatedFiles,
		redactor:        redactor,
	}, nil
}

// Start begins a new recording session.
// It rotates old files to ensure we only keep the last N traces.
func (r *Recorder) Start(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing file if any
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}

	// Rotate old files
	if err := r.rotate(); err != nil {
		return fmt.Errorf("rotate traces: %w", err)
	}

	// Create new file
	filename := fmt.Sprintf("trace_%s_%d.jsonl", sessionID, time.Now().UnixMilli())
	path := filepath.Join(r.basePath, filename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}

	r.file = f
	r.encoder = json.NewEncoder(f)
	r.currentPath = path
	return nil
}

// Log writes an event to the current trace file.
func (r *Recorder) Log(eventType, sessionID string, data interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.encoder == nil {
		return
	}

	evt := Event{
		Timestamp: time.Now(),
		Type:      eventType,
		SessionID: sessionID,
		Data:      r.redactor.Sanitize(data),
	}

	_ = r.encoder.Encode(evt)
}

// rotate keeps only the newest configured number of trace files.
func (r *Recorder) rotate() error {
	entries, err := os.ReadDir(r.basePath)
	if err != nil {
		return err
	}

	var traces []struct {
		Name string
		Time time.Time
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		traces = append(traces, struct {
			Name string
			Time time.Time
		}{e.Name(), info.ModTime()})
	}

	// Sort newest first
	sort.Slice(traces, func(i, j int) bool {
		return traces[i].Time.After(traces[j].Time)
	})

	maxFiles := r.maxRotatedFiles
	if maxFiles <= 0 {
		maxFiles = MaxRotatedFiles
	}

	// Delete excess
	if len(traces) >= maxFiles {
		// Keep N-1 to make room for the new one
		keep := maxFiles - 1
		if keep < 0 {
			keep = 0
		}
		for i := keep; i < len(traces); i++ {
			path := filepath.Join(r.basePath, traces[i].Name)
			_ = os.Remove(path)
		}
	}
	return nil
}

// CurrentPath returns the active or last-written trace file path.
func (r *Recorder) CurrentPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentPath
}

// Close finishes the current recording.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		r.encoder = nil
		return err
	}
	return nil
}
