package gojinja2

import (
	"strconv"
	"strings"
)

// parsePyInt 复刻 int(str, base): 允许下划线与 0x/0o/0b 前缀.
func parsePyInt(s string, base int) (int64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	if s == "" {
		return 0, false
	}
	if base == 10 {
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil
	}
	// 带前缀的非十进制
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	lower := strings.ToLower(s)
	switch base {
	case 16:
		lower = strings.TrimPrefix(lower, "0x")
	case 8:
		lower = strings.TrimPrefix(lower, "0o")
	case 2:
		lower = strings.TrimPrefix(lower, "0b")
	case 0:
		n, err := strconv.ParseInt(s, 0, 64)
		return n, err == nil
	}
	n, err := strconv.ParseInt(lower, base, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// parsePyFloat 复刻 float(str).
func parsePyFloat(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}
