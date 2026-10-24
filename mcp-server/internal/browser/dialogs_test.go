package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
)

// Holding this acknowledgement reproduces the interval in which Chrome has
// resumed an input command but the dialog handler has not published its record.
type gatedDialogClient struct {
	answerSent chan struct{}
	releaseAck chan struct{}
}

func (c *gatedDialogClient) Event() <-chan *cdp.Event { return nil }

func (c *gatedDialogClient) Call(ctx context.Context, session, method string, params interface{}) ([]byte, error) {
	if method != "Page.handleJavaScriptDialog" {
		return nil, fmt.Errorf("unexpected CDP method %s", method)
	}
	close(c.answerSent)
	select {
	case <-c.releaseAck:
		return []byte(`{}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type dialogWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *dialogWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestDialogReportWaitsForPublicationAfterAnswer(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	client := &gatedDialogClient{answerSent: make(chan struct{}), releaseAck: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page := rod.New().Context(ctx).Client(client).PageFromSession("dialog-session")
	started := time.Now()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		manager.handleDialog(ctx, "dialog-session", page, &proto.PageJavascriptDialogOpening{Type: proto.PageDialogTypeConfirm, Message: "Delete item password=dialog-secret"})
	}()
	select {
	case <-client.answerSent:
	case <-ctx.Done():
		t.Fatal("the handler never sent its answer")
	}
	if records := manager.DialogsSince("dialog-session", started); len(records) != 0 {
		t.Fatal("an unacknowledged answer was reported as complete")
	}
	// A canceled waiter must observe the pending publication, rather than
	// return success merely because no history record exists yet.
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := manager.AwaitDialogReports(canceled, "dialog-session", started); !errors.Is(err, context.Canceled) {
		t.Fatalf("pending report disappeared before acknowledgement: %v", err)
	}
	observed := &dialogWaitContext{Context: ctx, waiting: make(chan struct{})}
	waited := make(chan error, 1)
	go func() { waited <- manager.AwaitDialogReports(observed, "dialog-session", started) }()
	select {
	case <-observed.waiting:
	case <-ctx.Done():
		t.Fatal("the waiter never subscribed to publication")
	}
	select {
	case err := <-waited:
		t.Fatalf("report waiter returned before publication: %v", err)
	default:
	}
	close(client.releaseAck)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("publishing the dialog did not release its waiter")
	}
	<-handled
	records := manager.DialogsSince("dialog-session", started)
	if len(records) != 1 || records[0].Accepted || records[0].HandledBy != "policy" || records[0].Message != "Delete item password=[REDACTED]" {
		t.Fatalf("waiter did not receive the measured, redacted dismissal: %+v", records)
	}
}

func TestDialogReportWaitIsScopedToSessionAndOperation(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	started := time.Now()
	older := manager.beginDialogReport("session", started.Add(-time.Second))
	defer manager.completeDialogReport("session", older)
	other := manager.beginDialogReport("other-session", started)
	defer manager.completeDialogReport("other-session", other)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.AwaitDialogReports(ctx, "session", started); err != nil {
		t.Fatalf("unrelated pending dialogs delayed this operation: %v", err)
	}
	if err := manager.AwaitDialogReports(ctx, "no-dialog-session", started); err != nil {
		t.Fatalf("an action without dialogs must not wait: %v", err)
	}
}
