package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// keyChord is one key press with the modifiers held around it.
type keyChord struct {
	modifiers []input.Key
	key       input.Key
	label     string
}

var chordModifiers = map[string]input.Key{
	"control": input.ControlLeft, "ctrl": input.ControlLeft, "ctl": input.ControlLeft,
	"shift": input.ShiftLeft,
	"alt":   input.AltLeft, "option": input.AltLeft, "opt": input.AltLeft,
	"meta": input.MetaLeft, "cmd": input.MetaLeft, "command": input.MetaLeft,
	"super": input.MetaLeft, "win": input.MetaLeft, "os": input.MetaLeft,
}

var chordModifierLabels = map[input.Key]string{
	input.ControlLeft: "Control", input.ShiftLeft: "Shift", input.AltLeft: "Alt", input.MetaLeft: "Meta",
}

var namedKeys = map[string]input.Key{
	"enter": input.Enter, "return": input.Enter,
	"tab":    input.Tab,
	"escape": input.Escape, "esc": input.Escape,
	"backspace": input.Backspace,
	"delete":    input.Delete, "del": input.Delete,
	"space": input.Space, "spacebar": input.Space,
	"arrowup": input.ArrowUp, "up": input.ArrowUp,
	"arrowdown": input.ArrowDown, "down": input.ArrowDown,
	"arrowleft": input.ArrowLeft, "left": input.ArrowLeft,
	"arrowright": input.ArrowRight, "right": input.ArrowRight,
	"home": input.Home, "end": input.End,
	"pageup": input.PageUp, "pgup": input.PageUp,
	"pagedown": input.PageDown, "pgdn": input.PageDown,
	"insert": input.Insert, "ins": input.Insert,
	"contextmenu": input.ContextMenu,
	"f1":          input.F1, "f2": input.F2, "f3": input.F3, "f4": input.F4, "f5": input.F5, "f6": input.F6,
	"f7": input.F7, "f8": input.F8, "f9": input.F9, "f10": input.F10, "f11": input.F11, "f12": input.F12,
}

// namedKeyLabels are the canonical spellings used in results and messages.
var namedKeyLabels = map[input.Key]string{
	input.Enter: "Enter", input.Tab: "Tab", input.Escape: "Escape", input.Backspace: "Backspace",
	input.Delete: "Delete", input.Space: "Space", input.ArrowUp: "ArrowUp", input.ArrowDown: "ArrowDown",
	input.ArrowLeft: "ArrowLeft", input.ArrowRight: "ArrowRight", input.Home: "Home", input.End: "End",
	input.PageUp: "PageUp", input.PageDown: "PageDown", input.Insert: "Insert", input.ContextMenu: "ContextMenu",
}

// parseKeyChord reads "Enter", "a", "Control+a", "Shift+Tab", "Control+Shift+P"
// or "Meta++", plus any modifiers given separately ("Ctrl", "Shift", ...).
func parseKeyChord(spec string, extraModifiers []string) (keyChord, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return keyChord{}, fmt.Errorf("key is empty")
	}
	var parts []string
	switch {
	case spec == "+":
		parts = []string{"+"}
	case strings.HasSuffix(spec, "++"):
		parts = append(strings.Split(spec[:len(spec)-2], "+"), "+")
	default:
		parts = strings.Split(spec, "+")
	}
	keyPart := strings.TrimSpace(parts[len(parts)-1])
	modNames := append(append([]string{}, parts[:len(parts)-1]...), extraModifiers...)

	chord := keyChord{}
	seen := map[input.Key]bool{}
	labels := []string{}
	for _, raw := range modNames {
		name := strings.ToLower(strings.TrimSpace(raw))
		mod, ok := chordModifiers[name]
		if !ok {
			return keyChord{}, fmt.Errorf("unknown modifier %q in key %q; use Control, Shift, Alt or Meta", strings.TrimSpace(raw), spec)
		}
		if seen[mod] {
			continue
		}
		seen[mod] = true
		chord.modifiers = append(chord.modifiers, mod)
		labels = append(labels, chordModifierLabels[mod])
	}

	key, label, err := resolveChordKey(keyPart, spec)
	if err != nil {
		return keyChord{}, err
	}
	chord.key = key
	chord.label = strings.Join(append(labels, label), "+")
	return chord, nil
}

func resolveChordKey(keyPart, spec string) (input.Key, string, error) {
	lowered := strings.ToLower(keyPart)
	if k, ok := namedKeys[lowered]; ok {
		return k, namedKeyLabels[k], nil
	}
	if mod, ok := chordModifiers[lowered]; ok {
		return mod, chordModifierLabels[mod], nil
	}
	if len([]rune(keyPart)) == 1 && keyDefined(input.Key(keyPart[0])) && keyPart[0] < 128 {
		return input.Key(keyPart[0]), keyPart, nil
	}
	names := make([]string, 0, len(namedKeyLabels))
	for _, label := range namedKeyLabels {
		names = append(names, label)
	}
	sort.Strings(names)
	msg := fmt.Sprintf("unknown key %q", spec)
	if s := closestName(keyPart, names); s != "" {
		msg += fmt.Sprintf("; did you mean %q?", s)
	}
	return 0, "", fmt.Errorf("%s; use a named key (Enter, Tab, Escape, ArrowDown, F5, ...), a single character, or a chord like Control+a, Shift+Tab, Meta+k; type text with interact type or fill", msg)
}

// keyDefined reports whether rod has a key description for k; Key.Info
// panics on anything else.
func keyDefined(k input.Key) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	_ = k.Info()
	return true
}

// dispatchKeyChord presses the modifiers, types the key, and releases the
// modifiers, on page's context so a blocked page cannot hold the call. While a
// modifier other than Shift is held the key carries no text, which is how a
// browser tells a shortcut (Control+a selects all) from typing a character.
func dispatchKeyChord(page *rod.Page, chord keyChord) (err error) {
	bringToFront(page)
	mask := 0
	pressed := make([]input.Key, 0, len(chord.modifiers))
	defer func() {
		for i := len(pressed) - 1; i >= 0; i-- {
			mask &^= pressed[i].Modifier()
			if releaseErr := pressed[i].Encode(proto.InputDispatchKeyEventTypeKeyUp, mask).Call(page); releaseErr != nil && err == nil {
				err = releaseErr
			}
		}
	}()
	for _, mod := range chord.modifiers {
		mask |= mod.Modifier()
		if err = mod.Encode(proto.InputDispatchKeyEventTypeKeyDown, mask).Call(page); err != nil {
			return err
		}
		pressed = append(pressed, mod)
	}

	key := chord.key
	if mask&input.ModifierShift != 0 {
		if shifted, ok := key.Shift(); ok {
			key = shifted
		}
	}
	down := key.Encode(proto.InputDispatchKeyEventTypeKeyDown, mask)
	if mask&^input.ModifierShift != 0 {
		down.Type = proto.InputDispatchKeyEventTypeRawKeyDown
		down.Text = ""
		down.UnmodifiedText = ""
		if input.IsMac {
			if cmd := macEditingCommand(mask, key); cmd != "" {
				down.Commands = []string{cmd}
			}
		}
	}
	if err = down.Call(page); err != nil {
		return err
	}
	return key.Encode(proto.InputDispatchKeyEventTypeKeyUp, mask).Call(page)
}

// macEditingCommand maps the editing chords macOS routes through commands
// rather than key events.
func macEditingCommand(mask int, key input.Key) string {
	if mask&(input.ModifierMeta|input.ModifierControl) == 0 {
		return ""
	}
	switch strings.ToLower(key.Info().Key) {
	case "a":
		return "selectAll"
	case "c":
		return "copy"
	case "x":
		return "cut"
	case "v":
		return "paste"
	case "z":
		if mask&input.ModifierShift != 0 {
			return "redo"
		}
		return "undo"
	}
	return ""
}
