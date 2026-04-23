// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"strings"
	"unicode/utf8"
)

// TruncateBalanced shortens a rendered string to at most maxWidth runes.
//
//   - Strings not starting with '{' or '[' are tail-truncated with '…'.
//   - Objects/arrays are truncated between top-level items when possible:
//     the result stays a well-formed JSON literal whose final character is
//     the closing bracket, with a trailing ",…]" / ",…}" to flag truncation.
//   - If even one top-level item does not fit within budget (e.g. a single
//     long object inside a cell narrower than the object), we fall back to
//     plain tail-truncation so the user still sees content. That violates
//     the "always valid JSON" guarantee but is preferable to emitting a
//     deceptively empty "[]" or "{}".
//
// maxWidth <= 0 or short input → returned unchanged.
func TruncateBalanced(s string, maxWidth int) string {
	if maxWidth <= 0 || utf8.RuneCountInString(s) <= maxWidth {
		return s
	}
	first, _ := utf8.DecodeRuneInString(s)
	if first != '{' && first != '[' {
		return truncatePlain(s, maxWidth)
	}
	open := string(first)
	closeBracket := "}"
	if open == "[" {
		closeBracket = "]"
	}
	suffix := ",…" + closeBracket
	// Reserve 1 for the opening bracket + runes for suffix.
	budget := maxWidth - utf8.RuneCountInString(suffix) - 1
	if budget <= 0 {
		// Even "[,…]" does not fit. Fall back to plain truncate so the user
		// at least sees something rather than the ambiguous "[]".
		return truncatePlain(s, maxWidth)
	}

	depth := 0
	inString := false
	escape := false
	lastSafeEnd := -1 // byte index of the safe cut point in s
	runeCount := 0    // body runes consumed so far (excludes opening bracket)
	i := utf8.RuneLen(first)
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if runeCount+1 > budget {
			break
		}
		if escape {
			escape = false
		} else if inString {
			switch r {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
		} else {
			switch r {
			case '"':
				inString = true
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					// End of a complete top-level item. Safe to cut *after*
					// this bracket so the retained body is a full item.
					lastSafeEnd = i + w
				}
			case ',':
				if depth == 0 {
					// Between top-level items. Cut before the comma; the
					// suffix supplies its own.
					lastSafeEnd = i
				}
			}
		}
		runeCount++
		i += w
	}

	if lastSafeEnd > 0 {
		body := s[1:lastSafeEnd]
		return open + body + suffix
	}
	// Budget is too small for any complete top-level item (e.g. single long
	// object in a narrow cell). Fall back to plain tail-truncate so the user
	// sees the first fields instead of an empty-looking "[]".
	return truncatePlain(s, maxWidth)
}

func truncatePlain(s string, maxWidth int) string {
	if utf8.RuneCountInString(s) <= maxWidth {
		return s
	}
	keep := maxWidth - 1
	if keep <= 0 {
		return "…"
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == keep {
			break
		}
		b.WriteRune(r)
		count++
	}
	b.WriteRune('…')
	return b.String()
}
