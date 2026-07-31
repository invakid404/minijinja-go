package minijinja

import (
	"github.com/invakid404/minijinja-go/v2/internal/errors"
	"github.com/invakid404/minijinja-go/v2/value"
)

// Error represents an error that occurred during template processing.
type Error = errors.Error

// ErrorKind describes the type of error that occurred during template processing.
type ErrorKind = errors.ErrorKind

const (
	ErrSyntax           = errors.ErrSyntax
	ErrUndefinedVar     = errors.ErrUndefinedVar
	ErrUnknownFilter    = errors.ErrUnknownFilter
	ErrUnknownTest      = errors.ErrUnknownTest
	ErrUnknownFunction  = errors.ErrUnknownFunction
	ErrInvalidOperation = errors.ErrInvalidOperation
	ErrTemplateNotFound = errors.ErrTemplateNotFound
	ErrBadEscape        = errors.ErrBadEscape
	ErrUnknownBlock     = errors.ErrUnknownBlock
	ErrMissingArgument  = errors.ErrMissingArgument
	ErrTooManyArguments = errors.ErrTooManyArguments
	ErrBadInclude       = errors.ErrBadInclude
	ErrOutOfFuel        = errors.ErrOutOfFuel
	ErrEvalBlock        = errors.ErrEvalBlock
	ErrCannotUnpack     = errors.ErrCannotUnpack
	ErrUnknownMethod    = errors.ErrUnknownMethod
)

// NewError creates a new error with the given kind and message.
func NewError(kind ErrorKind, msg string) *Error {
	return errors.NewError(kind, msg)
}

// NewErrorWithoutDetail creates an error with no detail at all, which renders
// as just the kind — the engine's `Error::from(ErrorKind)`. An empty message
// passed to NewError is a different thing: it is a detail that happens to be
// empty, and it still renders its separator.
func NewErrorWithoutDetail(kind ErrorKind) *Error {
	return errors.NewErrorWithoutDetail(kind)
}

func valueToNative(v value.Value) interface{} {
	switch v.Kind() {
	case value.KindUndefined, value.KindNone:
		return nil
	case value.KindBool:
		b, _ := v.AsBool()
		return b
	case value.KindNumber:
		if i, ok := v.AsInt(); ok && v.IsActualInt() {
			return i
		}
		f, _ := v.AsFloat()
		return f
	case value.KindString:
		s, _ := v.AsString()
		return s
	case value.KindSeq:
		items, _ := v.AsSlice()
		result := make([]interface{}, len(items))
		for i, item := range items {
			result[i] = valueToNative(item)
		}
		return result
	case value.KindMap:
		// Through the generic map surface (MapKeys + GetItem), not AsMap: a host
		// object that is a map by REPRESENTATION but not a Go map still serializes
		// its entries here, matching the engine's serde, which walks
		// `try_iter_pairs` for an `ObjectRepr::Map` (value/mod.rs:1714-1737).
		// AsMap only recognises a Go map or a MapGetter, so an enumerable class
		// map fell through to an empty `{}` — silent data loss. A non-enumerable
		// map has no pairs, so it stays `{}` on both sides.
		keys, _ := v.MapKeys()
		result := make(map[string]interface{}, len(keys))
		for _, k := range keys {
			result[k] = valueToNative(v.GetItem(value.FromString(k)))
		}
		return result
	default:
		return v.String()
	}
}
