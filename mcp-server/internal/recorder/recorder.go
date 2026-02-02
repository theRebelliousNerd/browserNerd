package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
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
	mu       sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	basePath string
}

// NewRecorder creates a recorder instance.
// It ensures the directory exists.
func NewRecorder(basePath string) (*Recorder, error) {
	if basePath == "" {
		basePath = TraceDir
	}
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, err
	}
	return &Recorder{
		basePath: basePath,
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	r.file = f
	r.encoder = json.NewEncoder(f)
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
		Data:      data,
	}

	_ = r.encoder.Encode(evt)
}

// rotate keeps only the newest MaxRotatedFiles.
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

	// Delete excess
	if len(traces) >= MaxRotatedFiles {
		// Keep N-1 to make room for the new one
		keep := MaxRotatedFiles - 1
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
