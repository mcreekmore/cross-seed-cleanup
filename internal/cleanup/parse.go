package cleanup

import "strings"

func splitSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for v := range strings.SplitSeq(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	return set
}
