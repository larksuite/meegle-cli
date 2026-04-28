// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"fmt"
	"net/url"
	"strings"
)

// DecodeResult is the structured output of `meegle url decode`.
// All optional fields use omitempty so the JSON stays compact per URL kind.
type DecodeResult struct {
	URLKind        string            `json:"url_kind"`
	Host           string            `json:"host,omitempty"`
	Pathname       string            `json:"pathname"`
	SimpleName     string            `json:"simple_name,omitempty"`
	WorkItemType   string            `json:"work_item_type,omitempty"`
	WorkItemID     string            `json:"work_item_id,omitempty"`
	ViewID         string            `json:"view_id,omitempty"`
	ChartID        string            `json:"chart_id,omitempty"`
	PluginKey      string            `json:"plugin_key,omitempty"`
	TeamID         string            `json:"team_id,omitempty"`
	TemplateID     string            `json:"template_id,omitempty"`
	User           string            `json:"user,omitempty"`
	BusinessName   string            `json:"business_name,omitempty"`
	SettingType    string            `json:"setting_type,omitempty"`
	EditStr        string            `json:"edit_str,omitempty"`
	IsResource     bool              `json:"is_resource,omitempty"`
	Query          map[string]string `json:"query,omitempty"`
	RedirectedFrom string            `json:"redirected_from,omitempty"`
	Raw            string            `json:"raw,omitempty"`
}

// URLKindUnknown marks a URL that none of the rules matched. Callers (skills,
// SOPs) should treat this as "I cannot help you with this URL — ask the user
// for a different link" rather than guessing.
const URLKindUnknown = "unknown"

// DecodeURL parses a Meegle / Feishu Project URL into structured fields.
// It never fails for well-formed URLs — a URL that matches no rule returns
// a result with URLKind == "unknown" so the caller can branch on kind
// instead of distinguishing parse errors from "not recognised".
func DecodeURL(raw string) (*DecodeResult, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("url is empty")
	}

	// Accept bare paths ("/workbench"), scheme-less hosts, and full URLs.
	parseInput := trimmed
	if !strings.HasPrefix(parseInput, "/") && !strings.Contains(parseInput, "://") {
		parseInput = "https://" + parseInput
	}
	u, err := url.Parse(parseInput)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	pathname := u.Path
	if pathname == "" {
		pathname = "/"
	}

	res := &DecodeResult{
		Host:     u.Host,
		Pathname: pathname,
		Raw:      raw,
	}
	if len(u.Query()) > 0 {
		res.Query = map[string]string{}
		for k, v := range u.Query() {
			if len(v) > 0 {
				res.Query[k] = v[0]
			}
		}
	}

	// Strip /meego prefix (legacy subpath) before rule matching.
	workPath := pathname
	if strings.HasPrefix(workPath, "/meego/") || workPath == "/meego" {
		workPath = strings.TrimPrefix(workPath, "/meego")
		if workPath == "" {
			workPath = "/"
		}
		res.RedirectedFrom = pathname
	}

	// Resolve path aliases (old → new). A single hop is enough: aliases map
	// to canonical paths that are themselves leaves of the rule table, so
	// two-hop chains are a bug we want to catch, not tolerate.
	if target, params, ok := matchAlias(workPath); ok {
		if res.RedirectedFrom == "" {
			res.RedirectedFrom = workPath
		}
		workPath = applyTemplate(target, params)
	}

	// Match main rules, most-specific first. The rule table is ordered so
	// preset path segments (user-gantt, chart, …) win over the generic
	// :work_item_type slot.
	if matched := matchRule(workPath, res); matched {
		return res, nil
	}

	res.URLKind = URLKindUnknown
	return res, nil
}

// rule describes a single path pattern that maps to a url_kind. Segments
// starting with ':' are parameters; the remaining literals must match
// exactly. Patterns MUST NOT contain regex or wildcards — use multiple
// rules for alternation.
type rule struct {
	Pattern string
	Kind    string
	// Fixed extras written into the result regardless of path parameters.
	// Used only where a literal path segment carries semantic meaning
	// (e.g. the /story/ in /:simple_name/story/detail/:id would be caught
	// by a generic rule, but we keep things explicit by listing the types
	// as a :work_item_type parameter instead).
	Extras map[string]string
}

// matchRule walks the rule table and writes the matched parameters into res.
// Returns true if a rule matched.
//
// A pattern ending in "*" matches any non-empty suffix. This exists for
// frontend routes that are declared non-exact (e.g. /:project/setting) and
// render their subpages via a child router inside a React component — the
// subpage list is a black box and new pages get added without touching
// base.config.tsx, so an enumerated rule per subpage is a losing game. The
// wildcard rule must come after every specific rule sharing the same prefix,
// since matchRule is first-match-wins.
func matchRule(path string, res *DecodeResult) bool {
	segs := splitPath(path)
	for _, r := range urlRules {
		pat := splitPath(r.Pattern)
		var matchLen int
		if len(pat) > 0 && pat[len(pat)-1] == "*" {
			// Wildcard requires at least one trailing segment beyond the fixed
			// prefix; bare prefix should be handled by a separate, more
			// specific rule.
			matchLen = len(pat) - 1
			if len(segs) < matchLen+1 {
				continue
			}
		} else {
			matchLen = len(pat)
			if len(segs) != matchLen {
				continue
			}
		}
		params := map[string]string{}
		ok := true
		for i := 0; i < matchLen; i++ {
			p := pat[i]
			if strings.HasPrefix(p, ":") {
				params[p[1:]] = segs[i]
				continue
			}
			if p != segs[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		res.URLKind = r.Kind
		for k, v := range r.Extras {
			applyParam(res, k, v)
		}
		for k, v := range params {
			applyParam(res, k, v)
		}
		// Normalise :work_item_type = _xxx_resource → xxx + is_resource.
		if stripped, ok := stripResourceWrap(res.WorkItemType); ok {
			res.WorkItemType = stripped
			res.IsResource = true
		}
		return true
	}
	return false
}

// alias maps an old/legacy path pattern to a canonical path template. The
// canonical target may contain the same parameter names the source used —
// they are substituted at resolve time. Targets are themselves valid inputs
// to the main rule table.
type alias struct {
	Pattern string
	Target  string
}

// matchAlias returns the canonical target, captured params, and a match flag.
func matchAlias(path string) (string, map[string]string, bool) {
	segs := splitPath(path)
	for _, a := range urlAliases {
		pat := splitPath(a.Pattern)
		if len(pat) != len(segs) {
			continue
		}
		params := map[string]string{}
		ok := true
		for i, p := range pat {
			if strings.HasPrefix(p, ":") {
				params[p[1:]] = segs[i]
				continue
			}
			if p != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			return a.Target, params, true
		}
	}
	return "", nil, false
}

// splitPath splits a URL path into segments, dropping the leading slash and
// any empty trailing segment. "/" returns []; "/a/b" returns ["a","b"].
func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// applyTemplate substitutes :param tokens in the target template with the
// values captured from the source pattern.
func applyTemplate(target string, params map[string]string) string {
	segs := splitPath(target)
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			if v, ok := params[s[1:]]; ok {
				segs[i] = v
			}
		}
	}
	return "/" + strings.Join(segs, "/")
}

// applyParam writes a named parameter into the right struct field. Unknown
// names are silently ignored so rule authors can carry hints without
// breaking the decode pipeline.
func applyParam(res *DecodeResult, name, value string) {
	switch name {
	case "simple_name":
		res.SimpleName = value
	case "work_item_type":
		res.WorkItemType = value
	case "work_item_id":
		res.WorkItemID = value
	case "view_id":
		res.ViewID = value
	case "chart_id":
		res.ChartID = value
	case "plugin_key":
		res.PluginKey = value
	case "team_id":
		res.TeamID = value
	case "template_id":
		res.TemplateID = value
	case "user":
		res.User = value
	case "business_name":
		res.BusinessName = value
	case "setting_type":
		res.SettingType = value
	case "edit_str":
		res.EditStr = value
	}
}

// stripResourceWrap converts "_xxx_resource" → "xxx" so downstream tools
// can look the work item type up without knowing about the resource wrap.
// The frontend's router does the same: see getOriginNameFromResource in
// packages/platform/config/router/base.config.tsx.
func stripResourceWrap(name string) (string, bool) {
	if len(name) < 3 {
		return name, false
	}
	if !strings.HasPrefix(name, "_") || !strings.HasSuffix(name, "_resource") {
		return name, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(name, "_"), "_resource")
	if inner == "" {
		return name, false
	}
	return inner, true
}
