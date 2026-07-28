package value

// This file holds the part of the Rust engine's comparison rules that is not
// numeric coercion: the ordering of value KINDS.
//
// The coercion itself — `ops::coerce`, `ops::as_f64`, `f64_total_cmp` and the
// engine's primitive integer conversion — lives in numeric.go, which ports it
// in exact i128 with the engine's per-repr round-trip rules. Comparison calls
// those directly rather than keeping a second copy of them; the copy that used
// to live here had no u64 or u128 arms, so it was strictly the weaker port.

// kindOrder is the rank of a value kind in the Rust engine's kind ordering,
// which is the declaration order of its ValueKind enum: undefined, none, bool,
// number, string, bytes, seq, map, iterable, plain, invalid.
//
// Ordering falls back to this whenever two values have different kinds, so the
// ranks are observable through `<` on mixed types.
func kindOrder(k ValueKind) int {
	switch k {
	case KindUndefined:
		return 0
	case KindNone:
		return 1
	case KindBool:
		return 2
	case KindNumber:
		return 3
	case KindString:
		return 4
	case KindBytes:
		return 5
	case KindSeq:
		return 6
	case KindMap:
		return 7
	case KindIterable:
		return 8
	case KindPlain, KindCallable:
		// The Rust engine has no callable kind: a function is a plain object.
		return 9
	case KindInvalid:
		return 10
	default:
		return 9
	}
}
