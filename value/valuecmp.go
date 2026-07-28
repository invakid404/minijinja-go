package value

// ObjectWithValueCmp lets an object compare itself against an arbitrary Value,
// not just against another object.
//
// This is the generic capability BoundaryML's engine fork adds over upstream
// MiniJinja (boundaryml/minijinja@8cfc770, "add value_cmp on top of
// custom_cmp"): before ordinary equality or ordering, the engine asks an object
// operand to compare itself with the other side, from either operand position.
// It is what makes a host object with a canonical identity behave like that
// identity in a template — an enum object that displays an alias but compares by
// its canonical variant value, for example — without the engine knowing anything
// about the host's types.
//
// The dispatch rules the engine follows are:
//
//   - Equality tries the left operand's ValueCmp, then the right operand's. A
//     result of 0 means equal. Because equality is symmetric, the result is not
//     negated for the right-hand attempt.
//   - Ordering tries the left operand's ValueCmp first and uses the result as
//     is, then the right operand's and negates it. This happens before kind
//     ordering, so an object can order itself against a string or a number.
//   - Declining (ok == false) is not "not equal": the engine continues to the
//     ordinary rules, so an object that only understands strings still gets
//     normal object comparison against other objects.
//
// Implementations must be consistent — declining for a type in one direction and
// answering in the other makes `a == b` and `b == a` disagree.
//
// Example: a host object that compares by a canonical value while displaying
// something else.
//
//	func (e *enumValue) ValueCmp(other Value) (int, bool) {
//	    if s, ok := other.AsString(); ok {
//	        return strings.Compare(e.canonical, s), true
//	    }
//	    if obj, ok := other.AsObject(); ok {
//	        if o, ok := obj.(*enumValue); ok {
//	            return strings.Compare(e.canonical, o.canonical), true
//	        }
//	    }
//	    return 0, false
//	}
//
// [ObjectWithCmp] remains the narrower object-to-object hook and is still used
// when neither side answers ValueCmp.
type ObjectWithValueCmp interface {
	Object
	// ValueCmp compares this object with any value.
	//
	// Returns (cmp, ok) where cmp is negative, zero or positive as this object
	// sorts before, equal to, or after other, and ok is false when this object
	// has no opinion about that value.
	ValueCmp(other Value) (cmp int, ok bool)
}

// valueCmpDispatch asks v's object, if it has one, to compare itself with other.
//
// This is the engine-side half of the hook: it is called from Value.Equal and
// Value.Compare before their ordinary rules.
func valueCmpDispatch(v, other Value) (int, bool) {
	obj, ok := v.AsObject()
	if !ok {
		return 0, false
	}
	if cmp, ok := obj.(ObjectWithValueCmp); ok {
		return cmp.ValueCmp(other)
	}
	// Rust's default `value_cmp` delegates to `custom_cmp` for object operands
	// and declines everything else, so an object that only implements the
	// narrow hook still compares against another object here.
	if otherObj, ok := other.AsObject(); ok {
		return CompareObjects(obj, otherObj)
	}
	return 0, false
}

// valueCmpBothWays runs the hook from the left operand and then, negated, from
// the right. reverse says whether the right-hand result should be negated: it is
// for ordering, and it is not for equality, where only "is it zero" is asked and
// negating would be noise.
func valueCmpBothWays(a, b Value, reverse bool) (int, bool) {
	if cmp, ok := valueCmpDispatch(a, b); ok {
		return cmp, true
	}
	if cmp, ok := valueCmpDispatch(b, a); ok {
		if reverse {
			return -cmp, true
		}
		return cmp, true
	}
	return 0, false
}
