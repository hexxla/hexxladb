package record

// ValidAt reports whether instant t (Unix nanoseconds UTC) falls inside the validity window v.
//
// Bounds use a half-open interval when both ends are present: [ValidFrom, ValidTo).
// A nil ValidFrom means no lower bound; a nil ValidTo means no upper bound.
// When both are nil, every t is valid.
func ValidAt(v ValidityWire, t int64) bool {
	if v.ValidFrom != nil && t < *v.ValidFrom {
		return false
	}
	if v.ValidTo != nil && t >= *v.ValidTo {
		return false
	}
	return true
}
