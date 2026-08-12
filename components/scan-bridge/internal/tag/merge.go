// Package tag resolves the effective Paperless tag-ID list for a scan by
// merging a profile's default tags with per-call caller overrides.
package tag

// Strategy names one of the supported tag-merge algebras.
type Strategy string

const (
	// StrategyAdd unions the profile defaults and the caller tags,
	// deduplicated, preserving first-occurrence order.
	StrategyAdd Strategy = "add"
	// StrategyOverride discards the profile defaults and uses only the
	// caller tags, deduplicated.
	StrategyOverride Strategy = "override"
	// StrategyRemove drops any profile default tag also present in the
	// caller tags. A caller tag absent from the defaults is a no-op.
	StrategyRemove Strategy = "remove"
)

// Merge resolves the effective Paperless tag-ID list for one scan.
//
// If callerTagIDs is empty, the result is exactly a copy of
// defaultTagIDs, regardless of strategy. Otherwise the effective
// strategy is profileStrategy overridden by callerStrategy, if
// callerStrategy is non-empty. An unrecognized strategy behaves like
// StrategyAdd rather than panicking.
func Merge(defaultTagIDs []int, profileStrategy Strategy, callerTagIDs []int, callerStrategy Strategy) []int {
	if len(callerTagIDs) == 0 {
		return append([]int(nil), defaultTagIDs...)
	}

	effective := profileStrategy
	if callerStrategy != "" {
		effective = callerStrategy
	}

	switch effective {
	case StrategyOverride:
		return dedup(callerTagIDs)
	case StrategyRemove:
		return remove(defaultTagIDs, callerTagIDs)
	case StrategyAdd:
		fallthrough
	default:
		return dedup(append(append([]int(nil), defaultTagIDs...), callerTagIDs...))
	}
}

// dedup returns ids with duplicates removed, preserving first-occurrence
// order.
func dedup(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// remove returns defaultIDs minus any ID also present in excludeIDs,
// preserving defaultIDs order. IDs in excludeIDs absent from defaultIDs
// are silently ignored.
func remove(defaultIDs, excludeIDs []int) []int {
	exclude := make(map[int]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		exclude[id] = struct{}{}
	}
	out := make([]int, 0, len(defaultIDs))
	for _, id := range defaultIDs {
		if _, ok := exclude[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}
