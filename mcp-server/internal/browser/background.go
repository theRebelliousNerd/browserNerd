package browser

import (
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// enableBackgroundRendering gives the renderer focus without activating the
// desktop window. Input and frame capture must never fall back to activation.
func enableBackgroundRendering(page *rod.Page) error {
	if err := (proto.EmulationSetFocusEmulationEnabled{Enabled: true}).Call(page.Timeout(5 * time.Second)); err != nil {
		return fmt.Errorf("enable background rendering: %w", err)
	}
	return nil
}
