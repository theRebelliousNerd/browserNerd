package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/ysmood/gson"
)

const InputTraceRetention = 60 * time.Second

type InputTraceOptions struct {
	DurationMS int `json:"duration_ms"`
	MaxEvents  int `json:"max_events"`
	MaxBytes   int `json:"max_trace_bytes"`
}

type InputTracePhase struct {
	CurrentTarget    string `json:"current_target"`
	Phase            int    `json:"phase"`
	Capture          bool   `json:"listener_capture"`
	DefaultPrevented bool   `json:"default_prevented"`
	CancelBubble     bool   `json:"cancel_bubble"`
}
type InputTraceEvent struct {
	ID           int      `json:"event_id"`
	Kind         string   `json:"kind"`
	AtMS         int      `json:"at_ms"`
	TargetRef    string   `json:"target_ref"`
	Path         []string `json:"path"`
	WithinTarget bool     `json:"within_target"`
	Trusted      bool     `json:"trusted"`
	KeyCode      string   `json:"key_code,omitempty"`
	Button       *int     `json:"button,omitempty"`
	Buttons      *int     `json:"buttons,omitempty"`
	X            *float64 `json:"x,omitempty"`
	Y            *float64 `json:"y,omitempty"`
	Modifiers    struct {
		Ctrl  bool `json:"ctrl"`
		Shift bool `json:"shift"`
		Alt   bool `json:"alt"`
		Meta  bool `json:"meta"`
	} `json:"modifiers"`
	Phases []InputTracePhase `json:"phases"`
	Final  struct {
		Confirmed        bool   `json:"confirmed"`
		DefaultPrevented *bool  `json:"default_prevented"`
		Propagation      string `json:"propagation"`
	} `json:"final"`
}
type InputTraceSnapshot struct {
	Active    bool   `json:"active"`
	Reason    string `json:"reason"`
	ElapsedMS int    `json:"elapsed_ms"`
	TargetRef string `json:"target_ref"`
	Nodes     []struct {
		Ref  string `json:"ref"`
		Kind string `json:"kind"`
	} `json:"nodes"`
	Events         []InputTraceEvent `json:"events"`
	EventCount     int               `json:"event_count"`
	PhaseCount     int               `json:"phase_count"`
	FinalConfirmed int               `json:"final_confirmed"`
	DroppedEvents  int               `json:"dropped_events"`
	DroppedPhases  int               `json:"dropped_phases"`
	NodeLimit      bool              `json:"node_limit"`
	PathLimit      bool              `json:"path_limit"`
	InvalidUpdates int               `json:"invalid_updates"`
}

// InputTrace owns one exact document target and its temporary CDP resources.
// Terminal evidence is a bounded Go copy, independent of the document lifetime.
type InputTrace struct {
	mu               sync.Mutex
	ID               string
	Options          InputTraceOptions
	page             *rod.Page
	object           *proto.RuntimeRemoteObject
	binding          string
	snapshot         InputTraceSnapshot
	closed           bool
	stopping         bool
	cleanupConfirmed bool
	done             chan struct{}
	ready            chan struct{}
	readyOnce        sync.Once
	finishOnce       sync.Once
	cancel           context.CancelFunc
	expires          time.Time
	retentionTimer   *time.Timer
	invalidUpdates   int
}

func (m *SessionManager) ArmInputTrace(ctx context.Context, sessionID string, root *rod.Element, options InputTraceOptions) (*InputTrace, error) {
	ctx, cancelArm := context.WithTimeout(ctx, 3*time.Second)
	defer cancelArm()
	if options.DurationMS < 100 || options.DurationMS > 30000 || options.MaxEvents < 1 || options.MaxEvents > 256 || options.MaxBytes < 2048 || options.MaxBytes > 32768 || root == nil {
		return nil, fmt.Errorf("invalid bounded input trace options")
	}
	m.mu.Lock()
	record := m.sessions[sessionID]
	if record == nil || record.page == nil || record.streamContext == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("input trace requires a live owned session")
	}
	if root.Page().SessionID != record.page.SessionID || root.Page().Browser() != record.page.Browser() {
		m.mu.Unlock()
		return nil, fmt.Errorf("input trace target belongs to another session")
	}
	if record.inputTrace != nil && !record.inputTrace.terminal() {
		previous := record.inputTrace
		previous.mu.Lock()
		ending := previous.stopping || (previous.snapshot.TargetRef == "n0" && !previous.snapshot.Active)
		previous.mu.Unlock()
		m.mu.Unlock()
		if ending {
			select {
			case <-previous.done:
				return m.ArmInputTrace(ctx, sessionID, root, options)
			case <-ctx.Done():
				return nil, fmt.Errorf("previous input trace cleanup is still pending")
			}
		}
		return nil, fmt.Errorf("session already has an active input trace")
	}
	if record.inputTrace != nil {
		record.inputTrace.discard()
	}
	trace := &InputTrace{ID: uuid.NewString(), Options: options, page: record.page, binding: "__browsernerd_trace_" + uuid.NewString(), done: make(chan struct{}), ready: make(chan struct{})}
	record.inputTrace = trace
	lifetime := record.streamContext
	m.mu.Unlock()
	watchCtx, cancel := context.WithCancel(context.Background())
	trace.cancel = cancel
	messages := trace.page.Context(watchCtx).Event()
	if err := (proto.RuntimeAddBinding{Name: trace.binding}).Call(trace.page.Context(ctx)); err != nil {
		trace.finish("arm_failed", false)
		return nil, fmt.Errorf("input trace binding unavailable")
	}
	go trace.watch(watchCtx, lifetime, messages)
	object, err := root.Context(ctx).Evaluate(rod.Eval(inputTraceArmScript, options, trace.binding).ByObject())
	if err != nil || object == nil || object.ObjectID == "" {
		trace.finish("arm_failed", false)
		return nil, fmt.Errorf("input trace target could not be armed")
	}
	trace.mu.Lock()
	ending := trace.closed || trace.stopping
	if !ending {
		trace.object = object
	}
	trace.mu.Unlock()
	if ending {
		_, _ = (proto.RuntimeCallFunctionOn{ObjectID: object.ObjectID, FunctionDeclaration: `function(){return this.stop('arm_cancelled');}`, ReturnByValue: true}).Call(root.Page().Context(ctx))
		_ = root.Page().Context(ctx).Release(object)
		return nil, fmt.Errorf("input trace ended while arming")
	}
	select {
	case <-trace.ready:
	case <-ctx.Done():
		trace.finish("arm_cancelled", true)
		return nil, fmt.Errorf("input trace arm cancelled")
	}
	return trace, nil
}

func (m *SessionManager) InputTrace(sessionID, traceID string) (*InputTrace, error) {
	m.mu.RLock()
	record := m.sessions[sessionID]
	var trace *InputTrace
	if record != nil && record.page != nil {
		trace = record.inputTrace
	}
	m.mu.RUnlock()
	if trace == nil || trace.ID != traceID {
		return nil, fmt.Errorf("input trace is unavailable for this session")
	}
	trace.mu.Lock()
	expired := trace.closed && !trace.expires.IsZero() && time.Now().After(trace.expires)
	trace.mu.Unlock()
	if expired {
		return nil, fmt.Errorf("input trace retention expired")
	}
	return trace, nil
}

func (t *InputTrace) terminal() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.closed }

func (t *InputTrace) watch(ctx, lifetime context.Context, messages <-chan *rod.Message) {
	t.mu.Lock()
	page := t.page
	t.mu.Unlock()
	if page == nil {
		return
	}
	sessionID := page.SessionID
	lease := time.NewTimer(time.Duration(t.Options.DurationMS)*time.Millisecond + time.Second)
	defer lease.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-lifetime.Done():
			t.finish("session_closed", false)
			return
		case <-lease.C:
			t.finish("timeout", true)
			return
		case message, ok := <-messages:
			if !ok {
				t.finish("disconnected", false)
				return
			}
			if message.SessionID != sessionID {
				continue
			}
			switch message.Method {
			case "Runtime.bindingCalled":
				var event proto.RuntimeBindingCalled
				if !message.Load(&event) || event.Name != t.binding {
					continue
				}
				if t.accept(event.Payload, false) {
					t.mu.Lock()
					active := t.snapshot.Active
					reason := t.snapshot.Reason
					t.mu.Unlock()
					if !active {
						t.finish(reason, reason != "navigation")
						return
					}
				}
			case "Page.frameNavigated":
				var event proto.PageFrameNavigated
				if message.Load(&event) && event.Frame != nil && event.Frame.ParentID == "" {
					t.finish("navigation", false)
					return
				}
			case "Page.navigatedWithinDocument":
				t.finish("navigation", true)
				return
			case "Inspector.detached":
				t.finish("disconnected", false)
				return
			}
		}
	}
}

func (t *InputTrace) accept(payload string, final bool) bool {
	invalid := func() bool { t.mu.Lock(); t.invalidUpdates++; t.mu.Unlock(); return false }
	if len(payload) > t.Options.MaxBytes {
		return invalid()
	}
	var snapshot InputTraceSnapshot
	if json.Unmarshal([]byte(payload), &snapshot) != nil || !validTraceSnapshot(snapshot, t.Options) {
		return invalid()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.stopping && !final {
		return false
	}
	t.snapshot = snapshot
	t.readyOnce.Do(func() { close(t.ready) })
	return true
}

func (t *InputTrace) Stop() { t.finish("stopped", true) }

func (t *InputTrace) finish(reason string, readRuntime bool) {
	t.finishOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		t.mu.Lock()
		object := t.object
		t.stopping = true
		page := t.page
		t.mu.Unlock()
		confirmed := false
		if readRuntime && object != nil {
			result, err := (proto.RuntimeCallFunctionOn{ObjectID: object.ObjectID, FunctionDeclaration: `function(reason){return this.stop(reason);}`, Arguments: []*proto.RuntimeCallArgument{{Value: gson.New(reason)}}, ReturnByValue: true}).Call(page.Context(ctx))
			if err == nil && result.Result != nil {
				confirmed = t.accept(result.Result.Value.JSON("", ""), true)
			}
		}
		if object != nil {
			if err := page.Context(ctx).Release(object); err != nil {
				confirmed = false
			}
		}
		if err := (proto.RuntimeRemoveBinding{Name: t.binding}).Call(page.Context(ctx)); err != nil {
			confirmed = false
		}
		if _, err := page.Context(ctx).Eval(`(name)=>{delete window[name];}`, t.binding); err != nil {
			confirmed = false
		}
		t.mu.Lock()
		t.closed = true
		t.snapshot.Active = false
		if t.snapshot.Reason == "" {
			t.snapshot.Reason = reason
		}
		t.cleanupConfirmed = confirmed
		t.object = nil
		t.page = nil
		t.expires = time.Now().Add(InputTraceRetention)
		t.retentionTimer = time.AfterFunc(InputTraceRetention, func() { t.mu.Lock(); t.snapshot = InputTraceSnapshot{Reason: "expired"}; t.mu.Unlock() })
		t.mu.Unlock()
		t.cancel()
		close(t.done)
	})
}

func (t *InputTrace) discard() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.retentionTimer != nil {
		t.retentionTimer.Stop()
		t.retentionTimer = nil
	}
	t.snapshot = InputTraceSnapshot{Reason: "expired"}
	t.expires = time.Now().Add(-time.Second)
}

func (t *InputTrace) Snapshot() (InputTraceSnapshot, bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, _ := json.Marshal(t.snapshot)
	var snapshot InputTraceSnapshot
	_ = json.Unmarshal(raw, &snapshot)
	snapshot.InvalidUpdates = t.invalidUpdates
	return snapshot, t.closed, t.cleanupConfirmed
}

func validTraceSnapshot(s InputTraceSnapshot, o InputTraceOptions) bool {
	reasons := map[string]bool{"": true, "stopped": true, "timeout": true, "event_limit": true, "byte_limit": true, "phase_limit": true, "target_detached": true, "binding_unavailable": true, "navigation": true, "session_closed": true, "disconnected": true, "arm_cancelled": true, "arm_failed": true}
	kinds := map[string]bool{"pointerdown": true, "pointerup": true, "pointercancel": true, "mousedown": true, "mouseup": true, "click": true, "focus": true, "blur": true, "focusin": true, "focusout": true, "keydown": true, "keyup": true}
	keys := map[string]bool{"": true, "Enter": true, "NumpadEnter": true, "Space": true, "Tab": true, "Escape": true, "ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true}
	tags := map[string]bool{"element": true, "html": true, "body": true, "main": true, "section": true, "div": true, "span": true, "button": true, "input": true, "textarea": true, "select": true, "option": true, "a": true, "form": true, "label": true, "svg": true, "path": true}
	ref := func(value string) bool {
		if value == "window" || value == "document" || value == "unavailable" || value == "node_limit" {
			return true
		}
		if !strings.HasPrefix(value, "n") {
			return false
		}
		n, err := strconv.Atoi(strings.TrimPrefix(value, "n"))
		return err == nil && n >= 0 && n < 64 && value == fmt.Sprint("n", n)
	}
	if !reasons[s.Reason] || s.TargetRef != "n0" || len(s.Nodes) > 64 || len(s.Events) > o.MaxEvents || s.EventCount != len(s.Events) || s.ElapsedMS < 0 || s.ElapsedMS > 90000 || s.DroppedEvents < 0 || s.DroppedPhases < 0 {
		return false
	}
	for _, node := range s.Nodes {
		if !ref(node.Ref) || !tags[node.Kind] {
			return false
		}
	}
	phases, finals, previous := 0, 0, 0
	for _, event := range s.Events {
		if event.ID <= previous || !kinds[event.Kind] || !keys[event.KeyCode] || !ref(event.TargetRef) || len(event.Path) > 20 || len(event.Phases) > 40 || event.AtMS < 0 || event.AtMS > 90000 || event.Final.Propagation != "not_observable_after_dispatch" {
			return false
		}
		if event.Final.Confirmed != (event.Final.DefaultPrevented != nil) {
			return false
		}
		previous = event.ID
		for _, part := range event.Path {
			if !ref(part) {
				return false
			}
		}
		for _, phase := range event.Phases {
			if !ref(phase.CurrentTarget) || phase.Phase < 1 || phase.Phase > 3 {
				return false
			}
		}
		for _, number := range []*float64{event.X, event.Y} {
			if number != nil && (math.IsNaN(*number) || math.IsInf(*number, 0) || math.Abs(*number) > 1e6) {
				return false
			}
		}
		if event.Button != nil && (*event.Button < -1 || *event.Button > 4) || event.Buttons != nil && (*event.Buttons < 0 || *event.Buttons > 31) {
			return false
		}
		phases += len(event.Phases)
		if event.Final.Confirmed {
			finals++
		}
	}
	return phases == s.PhaseCount && finals == s.FinalConfirmed
}
