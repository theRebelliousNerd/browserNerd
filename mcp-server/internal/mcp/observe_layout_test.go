package mcp

import (
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/browser"

	"github.com/go-rod/rod/lib/proto"
)

func TestAnchorFromMatchedResolvesTheCascade(t *testing.T) {
	prop := func(name, value string, important bool) *proto.CSSCSSProperty {
		return &proto.CSSCSSProperty{Name: name, Value: value, Important: important}
	}
	rule := func(props ...*proto.CSSCSSProperty) *proto.CSSRuleMatch {
		return &proto.CSSRuleMatch{Rule: &proto.CSSCSSRule{Style: &proto.CSSCSSStyle{CSSProperties: props}}}
	}
	// Tailwind's inset-y-0 max-h-dvh: anchored at both ends and capped in dvh.
	sheet := anchorFromMatched(&proto.CSSGetMatchedStylesForNodeResult{MatchedCSSRules: []*proto.CSSRuleMatch{
		rule(prop("inset-block", "0px", false)), rule(prop("max-height", "100dvh", false)),
	}})
	if !sheet.TopSet || !sheet.BottomSet || !sheet.Dynamic() {
		t.Errorf("inset-block: 0 anchors both ends: %+v", sheet)
	}
	// h-screen, later overridden by an inline bottom.
	menu := anchorFromMatched(&proto.CSSGetMatchedStylesForNodeResult{
		MatchedCSSRules: []*proto.CSSRuleMatch{rule(prop("top", "0px", false), prop("height", "100vh", false))},
	})
	if menu.Dynamic() || !menu.LargeViewportSized() {
		t.Errorf("top: 0; height: 100vh is a toolbar trap: %+v", menu)
	}
	inline := anchorFromMatched(&proto.CSSGetMatchedStylesForNodeResult{
		MatchedCSSRules: []*proto.CSSRuleMatch{rule(prop("top", "0px", false), prop("height", "100vh", false))},
		InlineStyle:     &proto.CSSCSSStyle{CSSProperties: []*proto.CSSCSSProperty{prop("height", "100dvh", false)}},
	})
	if !inline.Dynamic() {
		t.Errorf("an inline 100dvh overrides the rule: %+v", inline)
	}
	// !important in a rule beats a later normal declaration and the inline style.
	important := anchorFromMatched(&proto.CSSGetMatchedStylesForNodeResult{
		MatchedCSSRules: []*proto.CSSRuleMatch{rule(prop("height", "100vh", true)), rule(prop("height", "100dvh", false))},
		InlineStyle:     &proto.CSSCSSStyle{CSSProperties: []*proto.CSSCSSProperty{prop("height", "50dvh", false)}},
	})
	if important.Height != "100vh" {
		t.Errorf("!important wins the cascade: %+v", important)
	}
	// A disabled declaration does not count.
	disabled := anchorFromMatched(&proto.CSSGetMatchedStylesForNodeResult{
		MatchedCSSRules: []*proto.CSSRuleMatch{rule(&proto.CSSCSSProperty{Name: "bottom", Value: "0", Disabled: true})},
	})
	if disabled.BottomSet {
		t.Errorf("a disabled bottom does not anchor: %+v", disabled)
	}
}

func TestViewportOpValidation(t *testing.T) {
	cases := []struct {
		op   map[string]interface{}
		want string
	}{
		{map[string]interface{}{"type": "viewport", "preset": "phone"}, ""},
		{map[string]interface{}{"type": "viewport", "width": 375, "height": 667, "mobile": true}, ""},
		{map[string]interface{}{"type": "viewport", "reset": true}, ""},
		{map[string]interface{}{"type": "viewport"}, "needs a preset"},
		{map[string]interface{}{"type": "viewport", "reset": true, "preset": "phone"}, "reset takes no other field"},
		{map[string]interface{}{"type": "viewport", "preset": "phablet"}, "must be one of"},
		{map[string]interface{}{"type": "viewport", "width": 50, "height": 50}, "outside"},
		{map[string]interface{}{"type": "viewport", "preset": "phone", "toolbar_height": 500}, "toolbar_height"},
		{map[string]interface{}{"type": "viewport", "preset": "phone", "zoom": 2}, "unknown field"},
	}
	for _, c := range cases {
		_, issues, _ := validateActOperations([]interface{}{c.op}, false)
		got := ""
		if len(issues) > 0 {
			got = issues[0].Message
		}
		if c.want == "" && got != "" {
			t.Errorf("%v: refused: %s", c.op, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%v: want an issue containing %q, got %q", c.op, c.want, got)
		}
	}
}

func TestResolveViewportPresetsAndOverrides(t *testing.T) {
	base := browser.Viewport{Width: 1920, Height: 1080, DeviceScaleFactor: 1}
	v, err := browser.ResolveViewport(browser.ViewportRequest{Preset: "phone"}, base)
	if err != nil || v.Width != 390 || v.Height != 844 || !v.Mobile || !v.Touch || v.ToolbarHeight != 86 || v.Preset != "phone" {
		t.Errorf("phone preset: %+v %v", v, err)
	}
	v, err = browser.ResolveViewport(browser.ViewportRequest{Preset: "phone", Orientation: "landscape"}, base)
	if err != nil || v.Width != 844 || v.Height != 390 {
		t.Errorf("landscape swaps the sides: %+v %v", v, err)
	}
	v, err = browser.ResolveViewport(browser.ViewportRequest{Preset: "pixel-7"}, base)
	if err != nil || v.Width != 412 || v.Height != 915 || v.DeviceScaleFactor != 2.625 || !v.Mobile {
		t.Errorf("pixel-7 preset: %+v %v", v, err)
	}
	w := 412
	v, err = browser.ResolveViewport(browser.ViewportRequest{Preset: "phone", Width: &w}, base)
	if err != nil || v.Width != 412 || v.Preset != "" || !v.Mobile {
		t.Errorf("a resized preset keeps its device traits and drops its name: %+v %v", v, err)
	}
	mobile := false
	v, _ = browser.ResolveViewport(browser.ViewportRequest{Preset: "tablet", Mobile: &mobile}, base)
	if v.ToolbarHeight != 0 {
		t.Errorf("a desktop-mode tablet has no phone toolbar: %+v", v)
	}
	if got := v.String(); !strings.Contains(got, "768x1024@2") {
		t.Errorf("String: %s", got)
	}
}
