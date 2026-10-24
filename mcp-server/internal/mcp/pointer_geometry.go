package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/go-rod/rod"
)

const pointerGeometryTimeout = 3 * time.Second
const pointerStabilityWindow = 60 * time.Millisecond

var errPointerTargetDetached = errors.New("element is detached")

type pointerPoint struct{ x, y float64 }

func measurePointerTarget(ctx context.Context, el *rod.Element, scroll bool) (pointerPoint, error) {
	res, err := el.Context(ctx).Eval(clickTargetScript, scroll)
	if err != nil {
		return pointerPoint{}, fmt.Errorf("could not reach the element: %w", err)
	}
	v := res.Value
	if !v.Get("error").Nil() {
		msg := v.Get("error").Str()
		if msg == "element is detached" {
			return pointerPoint{}, errPointerTargetDetached
		}
		if msg == "covered" {
			msg = "the element is covered by " + v.Get("by").Str()
		}
		return pointerPoint{}, errors.New(msg)
	}
	point := pointerPoint{x: v.Get("x").Num(), y: v.Get("y").Num()}
	if !v.Get("top").Bool() {
		shape, err := el.Context(ctx).Shape()
		if err != nil || shape == nil || shape.OnePointInside() == nil {
			return pointerPoint{}, fmt.Errorf("could not place the pointer inside a frame: %v", err)
		}
		point.x, point.y = shape.OnePointInside().X, shape.OnePointInside().Y
	}
	return point, nil
}

// prepareElementPointer never presses. Scroll and hover can each reposition a
// popup, so both phases require the same live hit target to remain stationary.
func prepareElementPointer(ctx context.Context, page *rod.Page, el *rod.Element) (float64, float64, error) {
	bounded, cancel := context.WithTimeout(ctx, pointerGeometryTimeout)
	defer cancel()
	var previous *pointerPoint
	var stableSince time.Time
	var lastErr error
	scroll, hovered := true, false
	for {
		point, err := measurePointerTarget(bounded, el, scroll)
		scroll = false
		if errors.Is(err, errPointerTargetDetached) {
			return 0, 0, err
		}
		if err != nil {
			lastErr, previous = err, nil
			stableSince = time.Time{}
		} else {
			changed := previous == nil || math.Abs(point.x-previous.x) > 0.5 || math.Abs(point.y-previous.y) > 0.5
			if changed {
				stableSince = time.Now()
				if hovered {
					if err := hoverAt(page.Context(bounded), point.x, point.y); err != nil {
						return 0, 0, fmt.Errorf("could not position native pointer on a stable target: %w", err)
					}
				}
			}
			previous = &point
			lastErr = nil
			if time.Since(stableSince) >= pointerStabilityWindow {
				if hovered {
					return point.x, point.y, nil
				}
				if err := hoverAt(page.Context(bounded), point.x, point.y); err != nil {
					return 0, 0, fmt.Errorf("could not position native pointer on a stable target: %w", err)
				}
				hovered, stableSince = true, time.Now()
			}
		}
		if err := sleepWithContext(bounded, 20*time.Millisecond); err != nil {
			if lastErr != nil {
				return 0, 0, fmt.Errorf("pointer target did not settle within %s: %w", pointerGeometryTimeout, lastErr)
			}
			return 0, 0, fmt.Errorf("element did not hold a stable, unobstructed pointer target within %s: %w", pointerGeometryTimeout, err)
		}
	}
}
