package templates

import "strings"

// Expand applies Bash-style brace expansion to a pattern.
//
//	"a/{b,c}"       → ["a/b", "a/c"]
//	"{a,b}/{x,y}"   → ["a/x", "a/y", "b/x", "b/y"]
//	"{a,{b,c}/d}"   → ["a", "b/d", "c/d"]
//	"a/b"           → ["a/b"]       (no braces — passthrough)
//	"a/{}/b"        → ["a//b"]      (empty branch preserved; caller should clean if needed)
//
// Unclosed braces are returned as a literal single-element slice.
func Expand(s string) []string {
	open := strings.IndexByte(s, '{')
	if open < 0 {
		return []string{s}
	}
	close := matchingClose(s, open)
	if close < 0 {
		return []string{s}
	}
	prefix := s[:open]
	inside := s[open+1 : close]
	suffix := s[close+1:]

	branches := splitTopLevelCommas(inside)
	var out []string
	for _, b := range branches {
		for _, combined := range Expand(prefix + b + suffix) {
			out = append(out, combined)
		}
	}
	return out
}

// ExpandMany applies Expand to each input pattern and flattens the result.
func ExpandMany(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		out = append(out, Expand(p)...)
	}
	return out
}

// matchingClose returns the index of the '}' matching the '{' at open,
// accounting for nested braces. Returns -1 if unclosed.
func matchingClose(s string, open int) int {
	depth := 1
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevelCommas splits s on commas that are not inside nested braces.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}
