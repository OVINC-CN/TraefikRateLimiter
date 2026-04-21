package parser

import "strings"

const (
	rulePathInt   = ":int"
	rulePathU64ID = ":u64id"
	rulePathHex64 = ":hex64"
	rulePathUUID  = ":uuid"
	rulePathHex32 = ":hex32"
)

func ParsePath(path string) string {
	if path == "" {
		return path
	}

	var b strings.Builder
	b.Grow(len(path))

	changed := false
	start := 0
	for i := 0; i <= len(path); i++ {
		if i < len(path) && path[i] != '/' {
			continue
		}
		if start < i {
			seg := path[start:i]
			if repl, ok := normalizeDynamicSegment(seg); ok {
				b.WriteString(repl)
				changed = true
			} else {
				b.WriteString(seg)
			}
		}
		if i < len(path) {
			b.WriteByte('/')
		}
		start = i + 1
	}

	if !changed {
		return path
	}
	return b.String()
}

func normalizeDynamicSegment(seg string) (string, bool) {
	n := len(seg)
	if n == 0 {
		return "", false
	}
	if isDigitsSegment(seg) {
		if n >= 16 {
			return rulePathU64ID, true
		}
		return rulePathInt, true
	}
	if n == 36 && isUUIDSegment(seg) {
		return rulePathUUID, true
	}
	if n == 32 && isHexSegment(seg) {
		return rulePathHex32, true
	}
	if n == 64 && isHexSegment(seg) {
		return rulePathHex64, true
	}
	return "", false
}

func isDigitsSegment(seg string) bool {
	for i := 0; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}

func isHexSegment(seg string) bool {
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

func isUUIDSegment(seg string) bool {
	if len(seg) != 36 {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
				continue
			}
			return false
		}
	}
	return true
}
