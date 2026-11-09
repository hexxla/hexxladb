package hexxladb

import (
	"slices"
	"strings"
)

// scoreCell computes relevance and reports whether the lexical filter matched.
// queryLow is already lowercased. Each whitespace-separated token contributes
// independently to the score.
func scoreCell(queryLow string, tags []string, content, sourceID string, confidence float64) (score float64, matched bool) {
	score = 0.1 * confidence // baseline confidence bonus (applied once)

	if queryLow == "" {
		return score, true
	}

	tagsLow := distinctFoldedTags(tags)
	contentLow := strings.ToLower(content)
	sourceIDLow := strings.ToLower(sourceID)

	for tok := range strings.FieldsSeq(queryLow) {
		// Tag scoring: each tag scores at most once per token (exact beats prefix).
		for _, tagLow := range tagsLow {
			switch {
			case tagLow == tok:
				score += 1.0
				matched = true
			case strings.HasPrefix(tagLow, tok):
				score += 0.8
				matched = true
			}
		}

		// Content scoring: verbatim match scores higher than case-insensitive.
		switch {
		case strings.Contains(content, tok):
			score += 0.6
			matched = true
		case strings.Contains(contentLow, tok):
			score += 0.5
			matched = true
		}

		// Source ID exact match per token.
		if sourceIDLow == tok {
			score += 0.3
			matched = true
		}
	}

	return score, matched
}

// distinctFoldedTags preserves input order for reproducible scoring and explanations.
func distinctFoldedTags(tags []string) []string {
	// Repeated tags count only once per token, regardless of case.
	tagsLow := make([]string, 0, len(tags))
	seenTags := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tagLow := strings.ToLower(tag)
		if _, seen := seenTags[tagLow]; seen {
			continue
		}
		seenTags[tagLow] = struct{}{}
		tagsLow = append(tagsLow, tagLow)
	}
	return tagsLow
}

// hasAllTags reports whether tags contains every element of required (case-sensitive).
func hasAllTags(tags, required []string) bool {
	for _, r := range required {
		if !slices.Contains(tags, r) {
			return false
		}
	}
	return true
}

// hasAnyTag reports whether tags contains at least one element of candidates (case-sensitive).
func hasAnyTag(tags, candidates []string) bool {
	for _, c := range candidates {
		if slices.Contains(tags, c) {
			return true
		}
	}
	return false
}
