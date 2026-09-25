package browser

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// DownloadRecord is one file a page downloaded. With allowAndName the file is
// saved in the download directory under its GUID; Path is that file.
type DownloadRecord struct {
	SessionID     string    `json:"session_id"`
	GUID          string    `json:"guid"`
	URL           string    `json:"url"`
	SuggestedName string    `json:"suggested_name"`
	Path          string    `json:"path"`
	State         string    `json:"state"` // started | completed | canceled
	ReceivedBytes int64     `json:"received_bytes"`
	TotalBytes    int64     `json:"total_bytes"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const maxDownloadHistory = 50

// downloadBook holds each session's recent downloads, and maps an in-flight
// download GUID to its session.
type downloadBook struct {
	mu       sync.Mutex
	byGUID   map[string]*DownloadRecord
	history  map[string][]*DownloadRecord
	listened map[*rod.Browser]bool
}

func (b *downloadBook) init() {
	if b.byGUID == nil {
		b.byGUID = make(map[string]*DownloadRecord)
		b.history = make(map[string][]*DownloadRecord)
		b.listened = make(map[*rod.Browser]bool)
	}
}

// DownloadsSince returns the downloads sessionID started at or after since,
// oldest first.
func (m *SessionManager) DownloadsSince(sessionID string, since time.Time) []DownloadRecord {
	m.downloads.mu.Lock()
	defer m.downloads.mu.Unlock()
	m.downloads.init()
	out := make([]DownloadRecord, 0)
	for _, rec := range m.downloads.history[sessionID] {
		if !rec.StartedAt.Before(since) {
			out = append(out, *rec)
		}
	}
	return out
}

// enableDownloads lets a browser context save downloads into the configured
// directory and emit download events. contextID is empty for the default
// context.
func (m *SessionManager) enableDownloads(browser *rod.Browser, contextID proto.BrowserBrowserContextID) {
	dir := m.cfg.GetDownloadDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("download directory %s unavailable: %v", dir, err)
		return
	}
	err := proto.BrowserSetDownloadBehavior{
		Behavior:         proto.BrowserSetDownloadBehaviorBehaviorAllowAndName,
		BrowserContextID: contextID,
		DownloadPath:     dir,
		EventsEnabled:    true,
	}.Call(browser)
	if err != nil {
		log.Printf("enable downloads: %v", err)
	}
}

// listenForDownloads subscribes once per browser connection to the
// browser-level download events and turns them into download facts on the
// session whose tab started the download.
func (m *SessionManager) listenForDownloads(browser *rod.Browser) {
	m.downloads.mu.Lock()
	m.downloads.init()
	if m.downloads.listened[browser] {
		m.downloads.mu.Unlock()
		return
	}
	m.downloads.listened[browser] = true
	m.downloads.mu.Unlock()

	wait := browser.EachEvent(
		func(ev *proto.BrowserDownloadWillBegin) {
			m.onDownloadWillBegin(ev)
		},
		func(ev *proto.BrowserDownloadProgress) {
			m.onDownloadProgress(ev)
		},
	)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("download listener panic recovered: %v", r)
			}
		}()
		wait()
	}()
}

// sessionForFrame finds the session whose tab's main frame is frameID. A page
// target's id is its main frame's id.
func (m *SessionManager) sessionForFrame(frameID proto.PageFrameID) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, record := range m.sessions {
		if record != nil && record.meta.TargetID == string(frameID) {
			return id
		}
	}
	return ""
}

func (m *SessionManager) onDownloadWillBegin(ev *proto.BrowserDownloadWillBegin) {
	sessionID := m.sessionForFrame(ev.FrameID)
	if sessionID == "" {
		return
	}
	now := time.Now()
	rec := &DownloadRecord{
		SessionID:     sessionID,
		GUID:          ev.GUID,
		URL:           m.redactor.SanitizeString(ev.URL),
		SuggestedName: m.redactor.SanitizeString(ev.SuggestedFilename),
		Path:          filepath.Join(m.cfg.GetDownloadDir(), ev.GUID),
		State:         "started",
		StartedAt:     now,
		UpdatedAt:     now,
	}
	m.downloads.mu.Lock()
	m.downloads.init()
	m.downloads.byGUID[ev.GUID] = rec
	h := append(m.downloads.history[sessionID], rec)
	if len(h) > maxDownloadHistory {
		for _, dropped := range h[:len(h)-maxDownloadHistory] {
			delete(m.downloads.byGUID, dropped.GUID)
		}
		h = h[len(h)-maxDownloadHistory:]
	}
	m.downloads.history[sessionID] = h
	m.downloads.mu.Unlock()
	m.emitDownloadFact(*rec, now)
}

func (m *SessionManager) onDownloadProgress(ev *proto.BrowserDownloadProgress) {
	state := ""
	switch ev.State {
	case proto.BrowserDownloadProgressStateCompleted:
		state = "completed"
	case proto.BrowserDownloadProgressStateCanceled:
		state = "canceled"
	}
	now := time.Now()
	m.downloads.mu.Lock()
	m.downloads.init()
	rec, ok := m.downloads.byGUID[ev.GUID]
	if ok {
		rec.ReceivedBytes = int64(ev.ReceivedBytes)
		rec.TotalBytes = int64(ev.TotalBytes)
		rec.UpdatedAt = now
		if state != "" {
			rec.State = state
			delete(m.downloads.byGUID, ev.GUID)
		}
	}
	var snapshot DownloadRecord
	if ok {
		snapshot = *rec
	}
	m.downloads.mu.Unlock()
	if ok && state != "" {
		m.emitDownloadFact(snapshot, now)
	}
}

func (m *SessionManager) emitDownloadFact(rec DownloadRecord, now time.Time) {
	if m.engine == nil {
		return
	}
	m.addFacts(context.Background(), rec.SessionID, "download", []mangle.Fact{{
		Predicate: "download",
		Args:      []interface{}{rec.SessionID, rec.GUID, rec.URL, rec.SuggestedName, rec.State, now.UnixMilli()},
		Timestamp: now,
	}})
}
