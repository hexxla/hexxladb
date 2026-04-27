package record

import (
	"sort"
	"strings"
)

// UniqueSortedTags returns deduplicated, trimmed, non-empty tags in stable sorted order.
// Suitable for deterministic secondary index writes and display.
func UniqueSortedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	var out []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
