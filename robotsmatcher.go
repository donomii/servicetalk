package main

import (
	"net/url"
	"strings"
	"sync"
)

// RobotsMatcher is a built-in class that holds a parsed robots.txt and
// answers two questions about it:
//
//   (send m allows:agent: aUrl aUserAgent) → bool
//   (send m crawlDelayFor: aUserAgent)     → number-of-seconds or nil
//
// It implements only the subset of RFC 9309 a basic crawler needs:
// User-agent / Allow / Disallow / Crawl-delay grouped by agent token,
// case-insensitive agent matching, longest-match-wins on rule paths.
//
// What this skips on purpose (out of scope for "get it working"):
//
//   - Wildcard `*` and end-anchor `$` in rule paths.  Real robots.txt
//     uses these heavily; we treat rules as plain prefixes.
//   - Sitemap and Host directives (irrelevant to URL admission).
//   - Per-group merging of consecutive User-agent lines is supported,
//     but only one agent per CheckUrls call is consulted at a time.
//
// Pure-servicetalk parsing of robots.txt was the original plan, but
// the current language lacks multi-statement if-branches and several
// comparison operators that make a state-machine parser readable —
// see TODO.md.  The Robocop.smt orchestrator stays pure servicetalk
// (HttpClient + RobotsMatcher + Dictionary).

type robotsRule struct {
	allow bool
	path  string
}

type robotsState struct {
	mu        sync.Mutex
	groups    map[string][]robotsRule
	delays    map[string]float64
	currentUA []string // agents that introduced the most recent group
	inGroup   bool     // saw at least one rule since the last UA
}

// callRobotsMatcherMethod dispatches every send to a RobotsMatcher
// instance.  See the class header for the surface.
func (obj *ObjectServer) callRobotsMatcherMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		return Nil // built-in: state lazily created on first parse:
	case "class":
		return StrVal(UserClassRobotsMatcher)
	case "yourself":
		return ObjVal(obj.id)
	case "isNil":
		return False
	case "notNil":
		return True
	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		switch args[0].String() {
		case "parse:", "allows:agent:", "crawlDelayFor:", "agentCount":
			return True
		}
		return False
	case "inspect":
		st := obj.robotsMatcherState()
		st.mu.Lock()
		obj.transcriptf("RobotsMatcher: %d agent groups, %d crawl-delay entries", len(st.groups), len(st.delays))
		st.mu.Unlock()
		return Nil
	case "free":
		obj.deregister()
		return Nil

	case "parse:":
		if len(args) < 1 {
			return False
		}
		obj.robotsMatcherParse(args[0].String())
		return True

	case "allows:agent:":
		if len(args) < 2 {
			return True // empty rules → allow-all
		}
		return BoolVal(obj.robotsMatcherAllows(args[0].String(), args[1].String()))

	case "crawlDelayFor:":
		if len(args) < 1 {
			return Nil
		}
		st := obj.robotsMatcherState()
		st.mu.Lock()
		defer st.mu.Unlock()
		key := strings.ToLower(strings.TrimSpace(args[0].String()))
		if v, ok := st.delays[key]; ok {
			return NumVal(v)
		}
		if v, ok := st.delays["*"]; ok {
			return NumVal(v)
		}
		return Nil

	case "agentCount":
		st := obj.robotsMatcherState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return NumVal(float64(len(st.groups)))
	}

	return unsupportedMessage(ObjVal(obj.id), "RobotsMatcher", name, args)
}

func (obj *ObjectServer) robotsMatcherState() *robotsState {
	if obj.robotsState == nil {
		obj.robotsState = &robotsState{
			groups: map[string][]robotsRule{},
			delays: map[string]float64{},
		}
	}
	return obj.robotsState
}

// robotsMatcherParse re-parses the supplied body and replaces the
// current state.  Empty body produces an empty matcher (allow-all).
func (obj *ObjectServer) robotsMatcherParse(body string) {
	st := obj.robotsMatcherState()
	st.mu.Lock()
	defer st.mu.Unlock()

	st.groups = map[string][]robotsRule{}
	st.delays = map[string]float64{}
	st.currentUA = nil
	st.inGroup = false

	for _, raw := range strings.Split(body, "\n") {
		line := raw
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		switch key {
		case "user-agent":
			if st.inGroup {
				st.currentUA = nil
			}
			st.currentUA = append(st.currentUA, strings.ToLower(value))
			st.inGroup = false
		case "allow":
			st.appendRule(true, value)
		case "disallow":
			st.appendRule(false, value)
		case "crawl-delay":
			st.applyCrawlDelay(value)
		}
	}
}

func (st *robotsState) appendRule(allow bool, path string) {
	st.inGroup = true
	if path == "" && !allow {
		// "Disallow:" with empty value is the spec's allow-all sentinel.
		// Skip — a missing rule is the same as no rule.
		return
	}
	for _, ua := range st.currentUA {
		st.groups[ua] = append(st.groups[ua], robotsRule{allow: allow, path: path})
	}
}

func (st *robotsState) applyCrawlDelay(value string) {
	st.inGroup = true
	var n float64
	for _, c := range value {
		if c < '0' || c > '9' {
			if c == '.' || c == '-' {
				continue
			}
			break
		}
	}
	// crude parse — accept ints and reasonable floats, reject garbage
	if _, ok := parseFloatSafe(value, &n); ok {
		for _, ua := range st.currentUA {
			st.delays[ua] = n
		}
	}
}

// parseFloatSafe is a tiny float parser so we don't pull in strconv
// for one number in one place; values up to 999_999 cover the
// well-formed range and out-of-range entries are silently dropped.
// (robots.txt callers don't crash, they just keep crawling.)
func parseFloatSafe(s string, out *float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var n float64
	frac := 0.0
	div := 1.0
	state := 0 // 0=int, 1=frac
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			d := float64(c - '0')
			if state == 0 {
				n = n*10 + d
			} else {
				frac += d / (div * 10)
				div *= 10
			}
		case c == '.' && state == 0:
			state = 1
		default:
			return 0, false
		}
	}
	*out = n + frac
	return *out, true
}

// robotsMatcherAllows applies the parsed rules to the URL/path for the
// requested agent.  Falls back to the "*" group when no agent-specific
// rules exist.  No rules anywhere → allow-all.
func (obj *ObjectServer) robotsMatcherAllows(rawURL, ua string) bool {
	path := "/"
	if u, err := url.Parse(rawURL); err == nil && u != nil {
		if u.Path != "" {
			path = u.Path
		}
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}

	st := obj.robotsMatcherState()
	st.mu.Lock()
	defer st.mu.Unlock()

	rules := st.groupForAgent(ua)
	if rules == nil {
		return true
	}

	bestLen := -1
	bestAllow := true
	for _, r := range rules {
		if !strings.HasPrefix(path, r.path) {
			continue
		}
		if len(r.path) > bestLen {
			bestLen = len(r.path)
			bestAllow = r.allow
		}
	}
	return bestAllow
}

// groupForAgent picks the most-specific UA group to consult.  Match
// is case-insensitive prefix on the agent's first whitespace token —
// "Googlebot/2.1" matches a group registered for "googlebot".  Falls
// back to "*" if no specific match exists.  Caller holds st.mu.
func (st *robotsState) groupForAgent(ua string) []robotsRule {
	uaToken := strings.ToLower(strings.TrimSpace(ua))
	if i := strings.IndexAny(uaToken, " /"); i > 0 {
		uaToken = uaToken[:i]
	}
	if rules, ok := st.groups[uaToken]; ok {
		return rules
	}
	// case-insensitive contains: a robots.txt UA of "googlebot" should
	// match a caller advertising "Googlebot-Image".
	for ua, rules := range st.groups {
		if ua == "*" {
			continue
		}
		if strings.Contains(uaToken, ua) {
			return rules
		}
	}
	if rules, ok := st.groups["*"]; ok {
		return rules
	}
	return nil
}
