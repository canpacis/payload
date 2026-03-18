package payload

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// UnmarshalerError is returned when a type's custom unmarshaler interface
// method (e.g. [HeaderUnmarshaler], [CookieUnmarshaler]) returns an error.
// It wraps the original error and records the raw string value that was being
// decoded along with the type of the unmarshaler that failed.
type UnmarshalerError struct {
	// Err is the underlying error returned by the unmarshaler.
	Err error
	// Value is the raw string that the unmarshaler was given.
	Value string
	// Unmarshaler is the reflect.Type of the struct field whose unmarshaler failed.
	Unmarshaler reflect.Type
}

// Error implements the error interface. Returns an empty string if Err is nil.
func (e *UnmarshalerError) Error() string {
	if e.Err == nil {
		return ""
	}

	return "payload: unmarshaler " + e.Unmarshaler.Name() + " failed to unmarshal value '" + e.Value + "': " + e.Err.Error()
}

// Unwrap returns the underlying error, allowing [errors.Is] and [errors.As]
// to inspect the error chain.
func (e *UnmarshalerError) Unwrap() error {
	return e.Err
}

// InvalidUnmarshalError describes an invalid argument passed to [decode].
// The argument must be a non-nil pointer to a struct; any other value
// (nil, non-pointer, pointer to non-struct) produces this error.
type InvalidUnmarshalError struct {
	// Type is the reflect.Type of the invalid argument, or nil if the
	// argument itself was nil.
	Type reflect.Type
}

// Error implements the error interface, reporting why the argument was invalid.
func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "payload: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Pointer {
		return "payload: Unmarshal(non-pointer " + e.Type.String() + ")"
	}

	if e.Type.Elem().Kind() != reflect.Struct {
		return "payload: Unmarshal(non-struct " + e.Type.String() + ")"
	}

	return "payload: Unmarshal(nil " + e.Type.String() + ")"
}

// UnmarshalTypeError describes a value that cannot be assigned or converted to
// a particular Go type. It mirrors the equivalent error in [encoding/json].
type UnmarshalTypeError struct {
	// Value is a human-readable description of the source value
	// (e.g. "bool", "array", "number -5").
	Value string
	// Type is the Go type that the value could not be assigned to.
	Type reflect.Type
	// Struct is the name of the struct containing the problematic field.
	Struct string
	// Field is the name of the struct field that could not be populated.
	Field string
}

// Error implements the error interface. When Struct and Field are set it
// produces a field-qualified message; otherwise it produces a type-only message.
func (e *UnmarshalTypeError) Error() string {
	typ := "<nil>"
	if e.Type != nil {
		typ = e.Type.String()
	}
	if e.Struct != "" || e.Field != "" {
		return "payload: cannot unmarshal " + e.Value + " into Go struct field " + e.Struct + "." + e.Field + " of type " + typ
	}
	return "payload: cannot unmarshal " + e.Value + " into Go value of type " + typ
}

// kinds maps primitive [reflect.Kind] values to string-parsing functions.
// Each function parses a raw string into the corresponding Go numeric type.
// It is used as a fallback when a target field has no registered type decoder
// and does not implement a source-specific unmarshaler interface.
var kinds = map[reflect.Kind]func(string) (any, error){
	reflect.Int: func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 0)
		if err != nil {
			return nil, err
		}
		return int(n), nil
	},
	reflect.Int8: func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 8)
		if err != nil {
			return nil, err
		}
		return int8(n), nil
	},
	reflect.Int16: func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 16)
		if err != nil {
			return nil, err
		}
		return int16(n), nil
	},
	reflect.Int32: func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(n), nil
	},
	reflect.Int64: func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	},
	reflect.Uint: func(s string) (any, error) {
		n, err := strconv.ParseUint(s, 10, 0)
		if err != nil {
			return nil, err
		}
		return uint(n), nil
	},
	reflect.Uint8: func(s string) (any, error) {
		n, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			return nil, err
		}
		return uint8(n), nil
	},
	reflect.Uint16: func(s string) (any, error) {
		n, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return nil, err
		}
		return uint16(n), nil
	},
	reflect.Uint32: func(s string) (any, error) {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return nil, err
		}
		return uint32(n), nil
	},
	reflect.Uint64: func(s string) (any, error) {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return uint64(n), nil
	},
	reflect.Float32: func(s string) (any, error) {
		f, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return nil, err
		}
		return float32(f), nil
	},
	reflect.Float64: func(s string) (any, error) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	},
	reflect.Bool: func(s string) (any, error) {
		ls := strings.ToLower(s)
		switch ls {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("parsing \"%s\": invalid syntax", s)
		}
	},
}

// types holds per-[reflect.Type] string-parsing functions registered via
// [RegisterType]. These take precedence over the built-in [kinds] parsers and
// are consulted before falling back to kind-level coercion.
var types = map[reflect.Type]func(string) (any, error){}

// RegisterType registers a custom string-parsing function for the concrete type
// of t. When [decode] encounters a struct field of that type and cannot satisfy
// it through direct assignment, conversion, or an unmarshaler interface, it
// calls decoder to convert the raw string into a value of type T.
//
// RegisterType is typically called during program initialisation (e.g. from an
// init function) for types such as time.Time or custom ID types that need
// bespoke parsing logic.
//
// Example:
//
//	payload.RegisterType(time.Time{}, func(s string) (time.Time, error) {
//	    return time.Parse(time.RFC3339, s)
//	})
func RegisterType[T any](t T, decoder func(string) (T, error)) {
	types[reflect.TypeOf(t)] = func(s string) (any, error) {
		return decoder(s)
	}
}

// sentinel errors used internally by [set] to signal the category of failure
// without allocating a full error value. They are translated into the
// appropriate exported error types by [decode].
var (
	// errType signals that the source value's type is incompatible with the
	// destination field and no coercion path was found.
	errType = errors.New("type error")
	// errInvalid signals that the destination field cannot be set (e.g.
	// unexported or otherwise unsettable via reflection).
	errInvalid = errors.New("invalid error")
)

// decode is the core reflection-driven decoder used by all source-specific
// decoders in this package. It iterates over the exported fields of the struct
// pointed to by v, looks up each field's value using getter (keyed by the
// field's struct tag named key), and assigns the result to the field.
//
// Type parameter T is the source-specific unmarshaler interface (e.g.
// [HeaderUnmarshaler]). When a field's type implements T, unmarshal is called
// to allow custom parsing logic; otherwise [set] attempts direct assignment,
// type conversion, a registered [RegisterType] parser, or a built-in [kinds]
// parser, in that order.
//
// decode returns [*InvalidUnmarshalError] if v is not a non-nil pointer to a
// struct, [*UnmarshalTypeError] if a field's type is incompatible with the
// source value, and [*UnmarshalerError] if a custom unmarshaler returns an
// error.
func decode[T any](v any, key string, getter func(string) any, unmarshal func(T, string) error) error {
	rv := reflect.ValueOf(v)

	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}

	rv = reflect.Indirect(rv)
	rt := rv.Type()
	if rv.Kind() != reflect.Struct {
		return &InvalidUnmarshalError{Type: rt}
	}

	for i := range rv.NumField() {
		field := rt.Field(i)
		value := rv.Field(i)

		if !field.IsExported() {
			continue
		}

		tag, ok := field.Tag.Lookup(key)
		if !ok {
			continue
		}

		target := getter(tag)
		if target == nil {
			continue
		}

		tv := reflect.ValueOf(target)
		if tv.IsZero() {
			continue
		}

		if err := set(value, tv, unmarshal); err != nil {
			if errors.Is(err, errType) {
				return &UnmarshalTypeError{
					Value:  tv.Type().Name(),
					Type:   field.Type,
					Struct: rt.Name(),
					Field:  field.Name,
				}
			} else if errors.Is(err, errInvalid) {
				return &InvalidUnmarshalError{Type: field.Type}
			} else {
				return &UnmarshalerError{
					Err:         err,
					Value:       tv.String(),
					Unmarshaler: field.Type,
				}
			}
		}
	}

	return nil
}

// set attempts to assign target to value using a cascade of strategies:
//
//  1. Direct assignment — if target's type is directly assignable to value's type.
//  2. Unmarshaler interface — if value (or a pointer to it) implements T,
//     unmarshal is called with the target's string representation.
//  3. Conversion — if target's type can be converted to value's type.
//  4. Registered type decoder — if a decoder has been registered for value's
//     exact type via [RegisterType], it is used to parse the string.
//  5. Built-in kind decoder — if value's kind has an entry in [kinds], it is
//     used to parse the string, and set is called recursively with the result.
//
// set returns [errType] if no coercion path exists, and [errInvalid] if value
// cannot be set. Any error from an unmarshaler or parser is returned directly
// and will be wrapped by [decode] into the appropriate exported error type.
func set[T any](value, target reflect.Value, unmarshal func(T, string) error) error {
	if !value.CanSet() {
		return errInvalid
	}
	if target.Type().AssignableTo(value.Type()) {
		value.Set(target)
		return nil
	}

	canT := true
	valueAsT, ok := value.Interface().(T)
	if !ok {
		valueAsT, ok = value.Addr().Interface().(T)
		if !ok {
			canT = false
		}
	}
	targetAsStr, ok := target.Interface().(string)
	if !ok {
		if target.Type().ConvertibleTo(value.Type()) {
			value.Set(target.Convert(value.Type()))
			return nil
		}
		return errType
	} else {
		if canT {
			return unmarshal(valueAsT, targetAsStr)
		}
		// Cannot unmarshal via interface; try registered type or kind parsers.
		typ, ok := types[value.Type()]
		if !ok {
			kind, ok := kinds[value.Type().Kind()]
			if !ok {
				return errType
			}
			coerced, err := kind(targetAsStr)
			if err != nil {
				return err
			}
			return set(value, reflect.ValueOf(coerced), unmarshal)
		}

		coerced, err := typ(targetAsStr)
		if err != nil {
			return err
		}
		return set(value, reflect.ValueOf(coerced), unmarshal)
	}
}
