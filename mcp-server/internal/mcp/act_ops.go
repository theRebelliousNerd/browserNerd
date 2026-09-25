package mcp

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The browser-act operation contract. Every operation type declares the
// fields it accepts, and a batch is validated as a whole before any operation
// runs: an unknown field, a wrong type, or a missing locator fails the batch
// with a message that names the fix, instead of being ignored and letting the
// operation run with a default the caller never asked for.

const (
	// maxSleepMs caps a sleep. Waiting on a condition belongs to the await ops.
	maxSleepMs = 60000
	// maxOpTimeoutMs caps any per-operation timeout_ms.
	maxOpTimeoutMs = 120000
)

type actFieldKind int

const (
	fieldString actFieldKind = iota
	fieldInt
	fieldNumber
	fieldBool
	fieldArray
	fieldScalar // string, number or bool, carried as a string
)

type actField struct {
	kind actFieldKind
	enum []string
}

type actOpSpec struct {
	fields   map[string]actField
	required []string
}

var (
	fStr    = actField{kind: fieldString}
	fInt    = actField{kind: fieldInt}
	fNum    = actField{kind: fieldNumber}
	fBool   = actField{kind: fieldBool}
	fArr    = actField{kind: fieldArray}
	fScalar = actField{kind: fieldScalar}
)

// interactActions are the actions an interact op performs.
var interactActions = []string{"click", "type", "select", "toggle", "clear", "hover"}

// actCommonFields are accepted on every operation.
var actCommonFields = map[string]actField{
	"type":        fStr,
	"dialog":      {kind: fieldString, enum: []string{"accept", "dismiss"}},
	"prompt_text": fStr,
}

// actLocatorFields find an element without a prior observe.
var actLocatorFields = map[string]actField{
	"ref":   fStr,
	"text":  fStr,
	"label": fStr,
	"role":  fStr,
	"name":  fStr,
	"index": fInt,
	"exact": fBool,
}

func withLocator(fields map[string]actField) map[string]actField {
	for k, v := range actLocatorFields {
		fields[k] = v
	}
	return fields
}

var actOpSpecs = map[string]actOpSpec{
	"navigate": {fields: map[string]actField{
		"url":        fStr,
		"wait_until": {kind: fieldString, enum: []string{"load", "networkidle", "none"}},
		"timeout_ms": fInt,
	}, required: []string{"url"}},
	"interact": {fields: withLocator(map[string]actField{
		"action":     {kind: fieldString, enum: interactActions},
		"x":          fNum,
		"y":          fNum,
		"value":      fScalar,
		"submit":     fBool,
		"timeout_ms": fInt,
	}), required: []string{"action"}},
	"fill": {fields: map[string]actField{
		"fields":        fArr,
		"submit":        fBool,
		"submit_button": fStr,
		"timeout_ms":    fInt,
	}, required: []string{"fields"}},
	"upload": {fields: map[string]actField{
		"ref":        fStr,
		"files":      fArr,
		"timeout_ms": fInt,
	}, required: []string{"ref", "files"}},
	"key": {fields: map[string]actField{
		"key":        fStr,
		"modifiers":  fArr,
		"timeout_ms": fInt,
	}, required: []string{"key"}},
	"history": {fields: map[string]actField{
		"action":     {kind: fieldString, enum: []string{"back", "forward", "reload"}},
		"timeout_ms": fInt,
	}, required: []string{"action"}},
	"sleep": {fields: map[string]actField{
		"duration_ms": fInt,
	}, required: []string{"duration_ms"}},
	"dialog": {fields: map[string]actField{
		"accept": fBool,
	}, required: []string{"accept"}},
	"session_create": {fields: map[string]actField{
		"url":        fStr,
		"browser_id": fStr,
		"isolated":   fBool,
	}},
	"session_attach": {fields: map[string]actField{
		"target_id":  fStr,
		"browser_id": fStr,
	}, required: []string{"target_id"}},
	"session_focus": {fields: map[string]actField{"session_id": fStr}},
	"session_close": {fields: map[string]actField{"session_id": fStr}},
	"session_fork": {fields: map[string]actField{
		"source_session_id": fStr,
		"url":               fStr,
	}},
	"browser_launch": {fields: map[string]actField{"new_instance": fBool}},
	"browser_close":  {fields: map[string]actField{"browser_id": fStr}},
	"wait": {fields: map[string]actField{
		"predicate":        fStr,
		"match_args":       fArr,
		"timeout_ms":       fInt,
		"poll_interval_ms": fInt,
	}, required: []string{"predicate"}},
	"await_stable": {fields: map[string]actField{
		"timeout_ms":      fInt,
		"network_idle_ms": fInt,
	}},
	"await_fact": {fields: map[string]actField{
		"predicate":  fStr,
		"args":       fArr,
		"timeout_ms": fInt,
	}, required: []string{"predicate"}},
	"await_conditions": {fields: map[string]actField{
		"conditions": fArr,
		"timeout_ms": fInt,
	}, required: []string{"conditions"}},
	"js": {fields: map[string]actField{
		"script":             fStr,
		"element_ref":        fStr,
		"timeout_ms":         fInt,
		"gate_reason":        fStr,
		"approved_by_handle": fStr,
		"result_mode":        {kind: fieldString, enum: []string{"scalar", "compact_json", "raw"}},
		"max_result_bytes":   fInt,
	}, required: []string{"script"}},
	"plan": {fields: map[string]actField{
		"actions":   fArr,
		"predicate": fStr,
		"delay_ms":  fInt,
	}},
}

// actOpTypes lists the operation types in a stable order for schemas and messages.
var actOpTypes = []string{
	"navigate", "interact", "fill", "upload", "key", "history", "sleep", "dialog",
	"browser_launch", "browser_close", "session_create", "session_attach", "session_focus",
	"session_close", "session_fork", "wait", "await_stable", "await_fact", "await_conditions",
	"js", "plan",
}

// actTypeAliases rewrite a natural shorthand type into its operation.
var actTypeAliases = map[string]struct{ opType, action string }{
	"click":  {"interact", "click"},
	"type":   {"interact", "type"},
	"select": {"interact", "select"},
	"toggle": {"interact", "toggle"},
	"clear":  {"interact", "clear"},
	"hover":  {"interact", "hover"},
	"press":  {"key", ""},
}

// actFieldAlias renames a field, scaling numeric values (seconds to ms).
type actFieldAlias struct {
	to    string
	scale float64
}

var actFieldAliases = map[string]map[string]actFieldAlias{
	"sleep": {
		"ms":           {"duration_ms", 1},
		"milliseconds": {"duration_ms", 1},
		"duration":     {"duration_ms", 1},
		"seconds":      {"duration_ms", 1000},
	},
	"session_fork": {"session_id": {"source_session_id", 1}},
	"*":            {"timeout": {"timeout_ms", 1}},
}

// actFieldHints answer a field that no operation accepts but callers reach for.
var actFieldHints = map[string]string{
	"selector": "CSS selectors are not accepted; locate with ref, text, label, role+name, or x/y",
	"css":      "CSS selectors are not accepted; locate with ref, text, label, role+name, or x/y",
	"xpath":    "XPath is not accepted; locate with ref, text, label, role+name, or x/y",
	"element":  "locate with ref, text, label, role+name, or x/y",
	"target":   "locate with ref, text, label, role+name, or x/y",
}

// actFillFieldKeys are the keys a fill.fields entry accepts.
var actFillFieldKeys = withLocator(map[string]actField{"value": fScalar})

// actOpIssue is one reason an operation was rejected before the batch ran.
type actOpIssue struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Message string `json:"error"`
}

// validateActOperations checks a whole batch and returns normalized copies of
// the operations, every problem found, and a note for each alias it applied.
// allowValueEnv admits value_env, which only browser-test resolves.
func validateActOperations(rawOps []interface{}, allowValueEnv bool) ([]map[string]interface{}, []actOpIssue, []string) {
	ops := make([]map[string]interface{}, 0, len(rawOps))
	var issues []actOpIssue
	var notes []string
	for idx, raw := range rawOps {
		op, ok := raw.(map[string]interface{})
		if !ok {
			issues = append(issues, actOpIssue{Index: idx, Type: "unknown", Message: "operation must be an object"})
			ops = append(ops, map[string]interface{}{})
			continue
		}
		normalized, opIssues, opNotes := validateActOperation(op, allowValueEnv)
		for _, msg := range opIssues {
			issues = append(issues, actOpIssue{Index: idx, Type: getStringFromMap(normalized, "type"), Message: msg})
		}
		for _, note := range opNotes {
			notes = append(notes, fmt.Sprintf("op %d: %s", idx, note))
		}
		ops = append(ops, normalized)
	}
	return ops, issues, notes
}

func validateActOperation(op map[string]interface{}, allowValueEnv bool) (map[string]interface{}, []string, []string) {
	out := make(map[string]interface{}, len(op))
	for k, v := range op {
		out[k] = v
	}
	var issues, notes []string

	rawType, _ := out["type"].(string)
	opType := strings.ToLower(strings.TrimSpace(rawType))
	if opType == "" {
		return out, []string{"missing type; use one of " + strings.Join(actOpTypes, ", ")}, nil
	}
	if alias, ok := actTypeAliases[opType]; ok {
		if alias.action != "" {
			if existing := getStringFromMap(out, "action"); existing != "" && !strings.EqualFold(existing, alias.action) {
				return out, []string{fmt.Sprintf("type %q conflicts with action %q", opType, existing)}, nil
			}
			out["action"] = alias.action
			notes = append(notes, fmt.Sprintf("type %s -> interact action %s", opType, alias.action))
		} else {
			notes = append(notes, fmt.Sprintf("type %s -> %s", opType, alias.opType))
		}
		opType = alias.opType
	}
	out["type"] = opType
	spec, known := actOpSpecs[opType]
	if !known {
		msg := fmt.Sprintf("unknown operation type %q", rawType)
		if s := closestName(opType, actOpTypes); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		} else {
			msg += "; use one of " + strings.Join(actOpTypes, ", ")
		}
		return out, []string{msg}, notes
	}

	accepts := func(name string) (actField, bool) {
		if f, ok := spec.fields[name]; ok {
			return f, true
		}
		f, ok := actCommonFields[name]
		return f, ok
	}

	// Field aliases first, so the rest of validation sees canonical names.
	for _, table := range []map[string]actFieldAlias{actFieldAliases[opType], actFieldAliases["*"]} {
		for from, alias := range table {
			value, present := out[from]
			if !present {
				continue
			}
			if _, targetOK := accepts(alias.to); !targetOK {
				continue
			}
			if _, both := out[alias.to]; both {
				issues = append(issues, fmt.Sprintf("give %q or %q, not both", alias.to, from))
				continue
			}
			if alias.scale != 1 {
				if n, ok := numberValue(value); ok {
					value = n * alias.scale
				}
			}
			delete(out, from)
			out[alias.to] = value
			notes = append(notes, fmt.Sprintf("%s %s -> %s", opType, from, alias.to))
		}
	}

	allowed := acceptedFieldNames(spec)
	present := make([]string, 0, len(out))
	for name := range out {
		present = append(present, name)
	}
	sort.Strings(present)
	for _, name := range present {
		value := out[name]
		if name == "value_env" && (opType == "interact") {
			if !allowValueEnv {
				issues = append(issues, "value_env is resolved only by browser-test; run this step in a browser-test fixture")
			} else if s, ok := value.(string); !ok || strings.TrimSpace(s) == "" {
				issues = append(issues, "value_env must name an environment variable")
			}
			continue
		}
		field, ok := accepts(name)
		if !ok {
			issues = append(issues, unknownFieldMessage(name, opType, allowed))
			continue
		}
		if msg := checkFieldValue(name, field, value, out); msg != "" {
			issues = append(issues, fmt.Sprintf("%s %s", opType, msg))
		}
	}
	for _, name := range spec.required {
		if _, ok := out[name]; !ok {
			issues = append(issues, fmt.Sprintf("%s needs %s", opType, name))
		}
	}
	if len(issues) > 0 {
		return out, issues, notes
	}
	issues = append(issues, validateOpSemantics(opType, out, allowValueEnv)...)
	return out, issues, notes
}

func acceptedFieldNames(spec actOpSpec) []string {
	names := make([]string, 0, len(spec.fields)+len(actCommonFields))
	for name := range spec.fields {
		names = append(names, name)
	}
	for name := range actCommonFields {
		if name != "type" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func unknownFieldMessage(name, opType string, allowed []string) string {
	msg := fmt.Sprintf("unknown field %q for %s", name, opType)
	if hint, ok := actFieldHints[strings.ToLower(name)]; ok {
		return msg + "; " + hint
	}
	if s := closestName(name, allowed); s != "" {
		return msg + fmt.Sprintf("; use %q", s)
	}
	for _, other := range actOpTypes {
		if _, ok := actOpSpecs[other].fields[name]; ok {
			return msg + fmt.Sprintf(" (%s belongs to %s); accepted: %s", name, other, strings.Join(allowed, ", "))
		}
	}
	return msg + "; accepted: " + strings.Join(allowed, ", ")
}

// checkFieldValue type-checks one field, normalizing a scalar value to a
// string in place so the executor reads it exactly.
func checkFieldValue(name string, field actField, value interface{}, op map[string]interface{}) string {
	switch field.kind {
	case fieldString:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%s must be a string", name)
		}
		if len(field.enum) > 0 {
			lowered := strings.ToLower(strings.TrimSpace(s))
			for _, e := range field.enum {
				if lowered == e {
					op[name] = e
					return ""
				}
			}
			return fmt.Sprintf("%s must be one of %s (got %q)", name, strings.Join(field.enum, ", "), s)
		}
	case fieldInt:
		n, ok := numberValue(value)
		if !ok || n != math.Trunc(n) {
			return fmt.Sprintf("%s must be a whole number", name)
		}
		if n < 0 {
			return fmt.Sprintf("%s must not be negative", name)
		}
		op[name] = n
	case fieldNumber:
		n, ok := numberValue(value)
		if !ok {
			return fmt.Sprintf("%s must be a number", name)
		}
		op[name] = n
	case fieldBool:
		if _, ok := value.(bool); !ok {
			return fmt.Sprintf("%s must be true or false", name)
		}
	case fieldArray:
		if _, ok := value.([]interface{}); !ok {
			return fmt.Sprintf("%s must be an array", name)
		}
	case fieldScalar:
		s, ok := scalarString(value)
		if !ok {
			return fmt.Sprintf("%s must be a string, number or boolean", name)
		}
		op[name] = s
	}
	return ""
}

func validateOpSemantics(opType string, op map[string]interface{}, allowValueEnv bool) []string {
	var issues []string
	if timeout, ok := op["timeout_ms"].(float64); ok && (timeout < 1 || timeout > maxOpTimeoutMs) {
		issues = append(issues, fmt.Sprintf("%s timeout_ms must be between 1 and %d", opType, maxOpTimeoutMs))
	}
	if _, hasPrompt := op["prompt_text"]; hasPrompt && opType != "dialog" && getStringFromMap(op, "dialog") == "" {
		issues = append(issues, fmt.Sprintf("%s prompt_text answers a dialog; add dialog: \"accept\"", opType))
	}
	switch opType {
	case "sleep":
		if ms, _ := op["duration_ms"].(float64); ms > maxSleepMs {
			issues = append(issues, fmt.Sprintf("sleep is capped at %d ms (got %.0f); wait on a condition with await_stable, await_fact or wait", maxSleepMs, ms))
		}
	case "interact":
		action := getStringFromMap(op, "action")
		issues = append(issues, validateLocator("interact", op, action == "click" || action == "hover")...)
		_, hasValue := op["value"]
		_, hasEnv := op["value_env"]
		if (action == "type" || action == "select") && !hasValue && !(allowValueEnv && hasEnv) {
			issues = append(issues, fmt.Sprintf("interact %s needs value", action))
		}
	case "fill":
		fields, _ := op["fields"].([]interface{})
		if len(fields) == 0 {
			issues = append(issues, "fill needs at least one entry in fields")
		}
		for i, raw := range fields {
			field, ok := raw.(map[string]interface{})
			if !ok {
				issues = append(issues, fmt.Sprintf("fill fields[%d] must be an object", i))
				continue
			}
			issues = append(issues, validateFillField(i, field, allowValueEnv)...)
		}
	case "key":
		if _, err := parseKeyChord(getStringFromMap(op, "key"), toStringSlice(op["modifiers"])); err != nil {
			issues = append(issues, err.Error())
		}
	case "upload":
		if len(toStringSlice(op["files"])) == 0 {
			issues = append(issues, "upload needs at least one path in files")
		}
	}
	return issues
}

func validateFillField(i int, field map[string]interface{}, allowValueEnv bool) []string {
	var issues []string
	names := make([]string, 0, len(actFillFieldKeys))
	for name := range actFillFieldKeys {
		names = append(names, name)
	}
	sort.Strings(names)
	keys := make([]string, 0, len(field))
	for name := range field {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		value := field[name]
		if name == "value_env" {
			if !allowValueEnv {
				issues = append(issues, fmt.Sprintf("fill fields[%d]: value_env is resolved only by browser-test; run this step in a browser-test fixture", i))
			}
			continue
		}
		spec, ok := actFillFieldKeys[name]
		if !ok {
			msg := fmt.Sprintf("fill fields[%d]: unknown key %q", i, name)
			if hint, hinted := actFieldHints[strings.ToLower(name)]; hinted {
				msg += "; " + hint
			} else if s := closestName(name, names); s != "" {
				msg += fmt.Sprintf("; use %q", s)
			} else {
				msg += "; accepted: " + strings.Join(names, ", ")
			}
			issues = append(issues, msg)
			continue
		}
		if msg := checkFieldValue(name, spec, value, field); msg != "" {
			issues = append(issues, fmt.Sprintf("fill fields[%d]: %s", i, msg))
		}
	}
	if len(issues) > 0 {
		return issues
	}
	for _, msg := range validateLocator("fill", field, false) {
		issues = append(issues, fmt.Sprintf("fields[%d]: %s", i, msg))
	}
	_, hasValue := field["value"]
	_, hasEnv := field["value_env"]
	if !hasValue && !(allowValueEnv && hasEnv) {
		issues = append(issues, fmt.Sprintf("fill fields[%d] needs value", i))
	}
	return issues
}

// validateLocator requires exactly one way to find the element.
func validateLocator(opType string, op map[string]interface{}, pointAllowed bool) []string {
	var issues []string
	count := 0
	for _, key := range []string{"ref", "text", "label", "role"} {
		if s := strings.TrimSpace(getStringFromMap(op, key)); s != "" {
			count++
		} else if _, present := op[key]; present {
			issues = append(issues, fmt.Sprintf("%s %s is empty", opType, key))
		}
	}
	_, hasX := op["x"]
	_, hasY := op["y"]
	if hasX != hasY {
		issues = append(issues, fmt.Sprintf("%s needs both x and y", opType))
	}
	if hasX && hasY {
		if !pointAllowed {
			issues = append(issues, fmt.Sprintf("%s x/y locates only click and hover; use ref, text, label or role+name", opType))
		}
		count++
	}
	if _, hasName := op["name"]; hasName && getStringFromMap(op, "role") == "" {
		issues = append(issues, fmt.Sprintf("%s name pairs with role (role+name); to match visible text use text", opType))
	}
	if _, hasIndex := op["index"]; hasIndex && getStringFromMap(op, "text") == "" && getStringFromMap(op, "label") == "" && getStringFromMap(op, "role") == "" {
		issues = append(issues, fmt.Sprintf("%s index picks among text, label or role matches", opType))
	}
	switch {
	case count == 0:
		issues = append(issues, fmt.Sprintf("%s needs a locator: ref, text, label, role(+name), or x/y", opType))
	case count > 1:
		issues = append(issues, fmt.Sprintf("%s takes one locator; give only one of ref, text, label, role, x/y", opType))
	}
	return issues
}

func numberValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	}
	return 0, false
}

func scalarString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	}
	if n, ok := numberValue(value); ok {
		return strconv.FormatFloat(n, 'f', -1, 64), true
	}
	return "", false
}

// closestName returns the candidate within edit distance 2 of name, or one
// that contains it, preferring the smallest distance.
func closestName(name string, candidates []string) string {
	name = strings.ToLower(name)
	best, bestDist := "", 3
	for _, c := range candidates {
		d := editDistance(name, strings.ToLower(c))
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	if best != "" {
		return best
	}
	if len(name) >= 3 {
		for _, c := range candidates {
			if strings.Contains(c, name) || strings.Contains(name, c) {
				return c
			}
		}
	}
	return ""
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			best := prev[j] + 1
			if v := cur[j-1] + 1; v < best {
				best = v
			}
			if v := prev[j-1] + cost; v < best {
				best = v
			}
			cur[j] = best
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// actOperationItemSchema documents every operation field for the calling
// model. Descriptions are short on purpose: schema text is paid on every turn.
func actOperationItemSchema() map[string]interface{} {
	str := func(desc string) map[string]interface{} {
		m := map[string]interface{}{"type": "string"}
		if desc != "" {
			m["description"] = desc
		}
		return m
	}
	typed := func(kind, desc string) map[string]interface{} {
		m := map[string]interface{}{"type": kind}
		if desc != "" {
			m["description"] = desc
		}
		return m
	}
	// Rare fields (match_args, args, conditions, script, target_id, browser_id,
	// isolated, ...) are accepted and named in the tool description.
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"type":          map[string]interface{}{"type": "string", "enum": actOpTypes},
			"url":           str(""),
			"wait_until":    map[string]interface{}{"type": "string", "enum": []string{"load", "networkidle", "none"}},
			"action":        str("interact: click|type|select|toggle|clear|hover; history: back|forward|reload"),
			"ref":           str(""),
			"text":          str("visible text or accessible name; exact, then contains"),
			"label":         str("form control label"),
			"role":          str("ARIA role, with name"),
			"name":          str(""),
			"index":         typed("integer", "0-based, when several match"),
			"exact":         typed("boolean", ""),
			"x":             typed("number", "viewport point, with y"),
			"y":             typed("number", ""),
			"value":         str("select: option value or label"),
			"submit":        typed("boolean", "Enter after type/fill"),
			"fields":        typed("array", ""),
			"submit_button": str("ref or text"),
			"key":           str("Enter, a, Control+a, Shift+Tab, Meta+k"),
			"duration_ms":   typed("integer", ""),
			"timeout_ms":    typed("integer", "max 120000"),
			"dialog":        map[string]interface{}{"type": "string", "enum": []string{"accept", "dismiss"}},
			"accept":        typed("boolean", ""),
			"prompt_text":   str(""),
			"files":         typed("array", ""),
			"predicate":     str(""),
			"session_id":    str("session_focus/close target"),
		},
		"required": []string{"type"},
	}
}
