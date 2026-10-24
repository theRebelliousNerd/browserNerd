package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
)

type backgroundFocusClient struct {
	t       *testing.T
	calls   int
	failure error
}

func (c *backgroundFocusClient) Event() <-chan *cdp.Event { return nil }
func (c *backgroundFocusClient) Call(_ context.Context, _, method string, params interface{}) ([]byte, error) {
	switch method {
	case "Target.setDiscoverTargets", "Page.enable":
		return []byte(`{}`), nil
	case "Target.attachToTarget":
		return []byte(`{"sessionId":"background-session"}`), nil
	}
	c.calls++
	request, ok := params.(proto.EmulationSetFocusEmulationEnabled)
	if method != "Emulation.setFocusEmulationEnabled" || !ok || !request.Enabled {
		c.t.Fatalf("unexpected background preparation: %s %+v", method, params)
	}
	return []byte(`{}`), c.failure
}

func TestBackgroundRenderingUsesEmulatedFocusAndPropagatesFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("focus emulation refused")} {
		client := &backgroundFocusClient{t: t, failure: failure}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		browser := rod.New().Context(ctx).NoDefaultDevice().Client(client)
		if err := browser.Connect(); err != nil {
			t.Fatal(err)
		}
		page, err := browser.PageFromTarget("background-target")
		if err != nil {
			t.Fatal(err)
		}
		err = enableBackgroundRendering(page)
		if !errors.Is(err, failure) {
			t.Fatalf("focus emulation failure was hidden: got %v want %v", err, failure)
		}
		if client.calls != 1 {
			t.Fatalf("background preparation made %d calls; no foreground fallback is allowed", client.calls)
		}
	}
}
