package mangle

import (
	"fmt"
	"strconv"
	"strings"
)

// Retention: when the buffer passes its limit it sheds the oldest LOW-signal
// facts first, then medium, and keeps the evidence of what went wrong -
// failures, errors, navigations, dialogs, toasts, user actions - until nothing
// else is left. A FIFO buffer under a polling app evicted a failed request
// seconds after it happened, because a steady stream of successful polls
// pushed it out.

// signalClass orders facts by what an investigation needs most.
type signalClass int

const (
	signalLow signalClass = iota
	signalMedium
	signalHigh
	signalClassCount
)

var signalClassNames = [signalClassCount]string{"low", "medium", "high"}

func (c signalClass) String() string {
	if c < 0 || c >= signalClassCount {
		return "unknown"
	}
	return signalClassNames[c]
}

// trimTarget is where a trim leaves the buffer: 90% of the limit, so the
// store rebuild after an eviction is paid once per limit/10 facts, not once
// per batch.
func trimTarget(limit int) int {
	slack := limit / 10
	if slack < 1 {
		slack = 1
	}
	return limit - slack
}

// requestKeyed lists the predicates that describe one request as
// (SessionId, RequestId, ...). A request's facts are retained or evicted as a
// unit, so a response is never kept without its request or the reverse.
var requestKeyed = map[string]bool{
	"net_request":           true,
	"net_response":          true,
	"net_header":            true,
	"net_correlation_key":   true,
	"request_initiator":     true,
	"request_payload_field": true,
	"net_http_error":        true,
	"net_loading_failed":    true,
	"net_failure_body":      true,
}

// highSignal predicates are kept until only high-signal facts remain.
var highSignal = map[string]bool{
	"navigation_event":   true,
	"current_url":        true,
	"page_load_failed":   true,
	"js_dialog":          true,
	"download":           true,
	"browser_log":        true,
	"toast_notification": true,
	"error_toast":        true,
	"warning_toast":      true,
	"click_event":        true,
	"user_click":         true,
	"user_type":          true,
	"user_select":        true,
	"user_toggle":        true,
	"user_upload":        true,
	"user_keypress":      true,
	"form_submitted":     true,
	"form_field_filled":  true,
	"history_navigation": true,
	"url_before_submit":  true,
}

// lowSignal predicates are page snapshots and bookkeeping: the next observe
// regenerates them, and a stale copy only misleads.
var lowSignal = map[string]bool{
	"dom_node":          true,
	"dom_attr":          true,
	"dom_text":          true,
	"dom_layout":        true,
	"dom_updated":       true,
	"dom_mapping":       true,
	"react_component":   true,
	"react_prop":        true,
	"react_state":       true,
	"disclosure_handle": true,
	"js_gate_open":      true,
	"confidence_score":  true,
	"screenshot_taken":  true,
	"js_evaluated":      true,
	"interactive":       true,
	"element_visible":   true,
	"element_enabled":   true,
	"element_value":     true,
	"elem_attr":         true,
	"elem_class":        true,
	"elem_bbox":         true,
	"nav_link":          true,
}

func argString(f Fact, i int) string {
	if i >= len(f.Args) {
		return ""
	}
	switch v := f.Args[i].(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	}
	return fmt.Sprintf("%v", f.Args[i])
}

func argInt(f Fact, i int) int64 {
	if i >= len(f.Args) {
		return 0
	}
	switch v := f.Args[i].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	}
	var n int64
	_, _ = fmt.Sscan(fmt.Sprintf("%v", f.Args[i]), &n)
	return n
}

func requestKey(f Fact) string {
	return argString(f, 0) + "\x00" + argString(f, 1)
}

// classifyFacts assigns each buffered fact its signal class. Request facts
// share their request's class: high when the request failed, low when it
// succeeded against a URL the session requests repeatedly (polling), medium
// otherwise.
func classifyFacts(facts []Fact) []signalClass {
	failed := make(map[string]bool)
	canceledOnly := make(map[string]bool)
	requestURL := make(map[string]string)
	urlCount := make(map[string]int)
	for _, f := range facts {
		switch f.Predicate {
		case "net_request":
			key := requestKey(f)
			url := argString(f, 0) + "\x00" + argString(f, 3)
			requestURL[key] = url
			urlCount[url]++
		case "net_response":
			if argInt(f, 2) >= 400 {
				failed[requestKey(f)] = true
			}
		case "net_http_error", "net_failure_body":
			failed[requestKey(f)] = true
		case "net_loading_failed":
			if argString(f, 3) == "true" {
				canceledOnly[requestKey(f)] = true
			} else {
				failed[requestKey(f)] = true
			}
		}
	}

	classes := make([]signalClass, len(facts))
	for i, f := range facts {
		classes[i] = classifyFact(f, failed, canceledOnly, requestURL, urlCount)
	}
	return classes
}

func classifyFact(f Fact, failed, canceledOnly map[string]bool, requestURL map[string]string, urlCount map[string]int) signalClass {
	if requestKeyed[f.Predicate] {
		key := requestKey(f)
		switch {
		case failed[key]:
			return signalHigh
		case canceledOnly[key]:
			return signalLow
		case urlCount[requestURL[key]] > 1:
			return signalLow
		default:
			return signalMedium
		}
	}
	if highSignal[f.Predicate] {
		return signalHigh
	}
	if lowSignal[f.Predicate] {
		return signalLow
	}
	switch f.Predicate {
	case "console_event":
		switch strings.ToLower(argString(f, 1)) {
		case "error", "warning", "warn", "assert":
			return signalHigh
		}
	case "ws_event":
		switch argString(f, 3) {
		case "error", "closed":
			return signalHigh
		}
	case "docker_log":
		switch strings.ToUpper(argString(f, 1)) {
		case "ERROR", "CRITICAL":
			return signalHigh
		}
	}
	return signalMedium
}

// evictionPlan marks the facts to drop so that len(facts)-dropped <= target:
// the oldest low-signal units first, then medium, then high. A request's
// facts form one unit and go together.
func evictionPlan(facts []Fact, classes []signalClass, target int) (drop []bool, dropped [signalClassCount]int) {
	drop = make([]bool, len(facts))
	need := len(facts) - target
	if need <= 0 {
		return drop, dropped
	}
	unitMembers := make(map[string][]int)
	for i, f := range facts {
		if requestKeyed[f.Predicate] {
			key := requestKey(f)
			unitMembers[key] = append(unitMembers[key], i)
		}
	}
	removed := 0
	for class := signalLow; class < signalClassCount && removed < need; class++ {
		for i, f := range facts {
			if removed >= need {
				break
			}
			if drop[i] || classes[i] != class {
				continue
			}
			if requestKeyed[f.Predicate] {
				for _, member := range unitMembers[requestKey(f)] {
					if !drop[member] {
						drop[member] = true
						dropped[classes[member]]++
						removed++
					}
				}
				continue
			}
			drop[i] = true
			dropped[class]++
			removed++
		}
	}
	return drop, dropped
}
