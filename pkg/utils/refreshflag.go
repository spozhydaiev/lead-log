package utils

import "strings"

func HasRefreshFlag(arg string) bool {
	arg = strings.TrimSpace(strings.ToLower(arg))
	return arg == "--refresh" || arg == "refresh" || arg == "-r"
}

func StripRefreshFlag(arg string) (clean string, refresh bool) {
	parts := strings.Fields(arg)
	var kept []string

	for _, p := range parts {
		lp := strings.ToLower(strings.TrimSpace(p))
		if lp == "--refresh" || lp == "refresh" || lp == "-r" {
			refresh = true
			continue
		}
		kept = append(kept, p)
	}

	return strings.Join(kept, " "), refresh
}
