package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
)

type backgroundInputClient struct {
	methods []string
}

func (c *backgroundInputClient) Event() <-chan *cdp.Event { return nil }
func (c *backgroundInputClient) Call(_ context.Context, _, method string, _ interface{}) ([]byte, error) {
	switch method {
	case "Target.setDiscoverTargets", "Page.enable":
		return []byte(`{}`), nil
	case "Target.attachToTarget":
		return []byte(`{"sessionId":"background-session"}`), nil
	}
	c.methods = append(c.methods, method)
	if method != "Input.dispatchMouseEvent" && method != "Input.dispatchKeyEvent" {
		return nil, fmt.Errorf("background input must not call %s", method)
	}
	return []byte(`{}`), nil
}

func TestBackgroundPointerAndKeysNeverActivateTarget(t *testing.T) {
	for _, operation := range []string{"hover", "click", "key"} {
		t.Run(operation, func(t *testing.T) {
			client := &backgroundInputClient{}
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
			switch operation {
			case "hover":
				err = hoverAt(page, 10, 20)
			case "click":
				err = dispatchClick(page, 10, 20, 1)
			case "key":
				chord, parseErr := parseKeyChord("Control+a", nil)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				err = dispatchKeyChord(page, chord)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(client.methods) == 0 {
				t.Fatal("input was not dispatched")
			}
			for _, method := range client.methods {
				if method != "Input.dispatchMouseEvent" && method != "Input.dispatchKeyEvent" {
					t.Fatalf("implicit focus/visibility call during %s: %s", operation, method)
				}
			}
		})
	}
}
