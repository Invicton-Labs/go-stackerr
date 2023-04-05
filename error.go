package stackerr

import (
	"encoding/json"
	"errors"
	"fmt"

	nativeStackErrors "github.com/pkg/errors"
)

const stackDivider string = "======================================"

type Error interface {
	error
	json.Marshaler
	json.Unmarshaler
	// ErrorWithStack returns a string that includes the error message
	// of the wrapped error, with the stack appended to it.
	ErrorWithStack() string
	// Stacks returns all stacks associated with this stackerr.Error,
	// ordered from most recent to oldest.
	Stacks() Stacks
	// FormatStack returns the stackerr.Error's stacks in a human-readable form.
	FormatStacks() string
	// FormatStackJson returns the stackerr.Error's stacks in JSON form.
	FormatStacksJson() string
	// Unwrap returns the error that this stackerr.Error is wrapping.
	Unwrap() error
	// Fields returns a map of key-value pairs that are associated with
	// this stackerr.Error.
	Fields() map[string]any
	// With adds one or more key-value pairs to this stackerr.Error, overwriting
	// any existing key-value pair with the same key.
	With(keyValuePairs map[string]any) Error
	// WithSingle adds a single key-value pair to this stackerr.Error, overwriting
	// any existing key-value pair with the same key. It is equivalent to calling
	// With with a single key/value in the map.
	WithSingle(key string, value any) Error
}

// A special interface that can be used to add key-value pairs in-place, without
// cloning the existing error.
type InPlaceEditError interface {
	Error
	// WithInPlace will add key-value pairs to an existing error
	// without making a clone of it first.
	WithInPlace(keyValuePairs map[string]any)
	// SetError will set the internal (wrapped) error for a stackerr.Error,
	// without cloning the original stackerr.Error first.
	SetError(err error)
	// SetError will set the stacks for a stackerr.Error,
	// without cloning the original stackerr.Error first.
	SetStacks(stacks Stacks)
}

type stackError struct {
	Err         error          `json:"err"`
	StackTraces Stacks         `json:"stack_traces"`
	MetaFields  map[string]any `json:"meta_fields"`
}

func (se *stackError) MarshalJSON() ([]byte, error) {
	return json.Marshal(se)
}

func (se *stackError) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, se)
}

func (se *stackError) clone() *stackError {
	newStackError := &stackError{
		Err:         se.Err,
		StackTraces: make(Stacks, len(se.StackTraces)),
		MetaFields:  map[string]any{},
	}
	copy(newStackError.StackTraces, se.StackTraces)
	for k, v := range se.MetaFields {
		newStackError.MetaFields[k] = v
	}
	return newStackError
}

func (se *stackError) ErrorWithStack() string {
	return se.Error() + "\n" + se.FormatStacks()
}

func (se *stackError) Error() string {
	return se.Err.Error()
}

func (se *stackError) Stacks() Stacks {
	return se.StackTraces
}

func (se *stackError) Unwrap() error {
	return se.Err
}

func (se *stackError) FormatStacks() string {
	return se.StackTraces.Format()
}

func (se *stackError) FormatStacksJson() string {
	b, _ := json.Marshal(se.StackTraces)
	return string(b)
}

func (se *stackError) With(keyValuePairs map[string]any) Error {
	newStackError := se.clone()
	for k, v := range keyValuePairs {
		newStackError.MetaFields[k] = v
	}
	return newStackError
}

func (se *stackError) WithSingle(key string, value any) Error {
	newStackError := se.clone()
	newStackError.MetaFields[key] = value
	return newStackError
}

func (se *stackError) WithInPlace(keyValuePairs map[string]any) {
	for k, v := range keyValuePairs {
		se.MetaFields[k] = v
	}
}

func (se *stackError) SetError(err error) {
	se.Err = err
}

func (se *stackError) SetStacks(stacks Stacks) {
	se.StackTraces = stacks
}

func (se *stackError) Fields() map[string]any {
	return se.MetaFields
}

// FromRecover converts a panic recover() result
// into a stackerr.Error, using the stack at the
// point where the panic was created.s
func FromRecover(r any) Error {
	if r == nil {
		return nil
	}
	switch e := r.(type) {
	case error:
		return new(e, 3, true)
	default:
		return new(fmt.Errorf("%v", r), 3, true)
	}
}

// Wrap wraps an error into a stackerr.Error, using
// the stack trace at the point where this function was called.
func Wrap(err error) Error {
	return new(err, 1, true)
}

// WrapWithFrameSkips wraps an error into a stackerr.Error, ignoring
// the most recent `skippedFrames` frames of the stack.
func WrapWithFrameSkips(err error, skippedFrames int) Error {
	return new(err, 1+skippedFrames, true)
}

// WrapWithStack wraps an error into a stackerr.Error, using
// the given stack as the stackerr.Error's stack trace.
func WrapWithStack(err error, stack Stack) Error {
	return new(err, 1, true, stack)
}

// WrapWithoutExtraStack wraps an error into a stackerr.Error. If the
// error being wrapped already has a stack, no additional stack will be
// added. If it doesn't, the current stack will be added.
func WrapWithoutExtraStack(err error) Error {
	return new(err, 1, false)
}

// WrapWithFrameSkipsWithoutExtraStack wraps an error into a stackerr.Error, ignoring
// the most recent `skippedFrames` frames of the stack. If the
// error being wrapped already has a stack, no additional stack will be
// added.
func WrapWithFrameSkipsWithoutExtraStack(err error, skippedFrames int) Error {
	return new(err, 1+skippedFrames, false)
}

type stackTracer interface {
	StackTrace() nativeStackErrors.StackTrace
}

func new(err error, skippedFrames int, addStackToExisting bool, newStacks ...Stack) Error {
	// If it's nil, just return nil, since it's not a real error
	if err == nil {
		return nil
	}

	numAllstacks := 1
	if len(newStacks) > numAllstacks {
		numAllstacks = len(newStacks)
	}
	allStacks := make([]Stack, 0, numAllstacks)

	allFields := map[string]any{}
	unwrapped := err
	for unwrapped != nil {
		// Check if it's a stack error
		if serr, ok := unwrapped.(*stackError); ok {
			allStacks = append(allStacks, serr.StackTraces...)
			for k, v := range serr.MetaFields {
				// Only add it if we don't already have the same key,
				// since we're starting with the outermost wrapper
				// (and outermost has key priority)
				if _, ok := allFields[k]; !ok {
					allFields[k] = v
				}
			}
			// Since any stack error will have already checked
			// wrapped errors below it, we can stop here.
			break
		} else if st, ok := unwrapped.(stackTracer); ok {
			// If it's an "github.com/pkg/errors" stack error, convert it
			stack := st.StackTrace()
			uintptrs := make([]uintptr, len(stack))
			for i, v := range stack {
				uintptrs[i] = uintptr(v)
			}
			allStacks = append(allStacks, uintptrToFrames(uintptrs))
		}

		unwrapped = errors.Unwrap(unwrapped)
	}

	// If there are any explicitly specified new stacks, add them
	if len(newStacks) > 0 {
		allStacks = append(newStacks, allStacks...)
	} else if len(allStacks) == 0 || addStackToExisting {
		// Otherwise, if there are no existing stacks OR we're supposed to force-add a new stack,
		// add the current stack
		allStacks = append([]Stack{StackTraceWithSkippedFrames(1 + skippedFrames)}, allStacks...)
	}

	if len(allStacks) > 1 {
		// Only include distinct stacks
		allStacks = NewStacks(allStacks).RemoveParents()
	}

	// If we're wrapping something that's already a stack error,
	// don't double wrap it.
	if serr, ok := err.(*stackError); ok {
		return &stackError{
			serr.Err,
			allStacks,
			allFields,
		}
	}

	// Otherwise, create a new stack error
	return &stackError{
		err,
		allStacks,
		allFields,
	}
}

func Errorf(format string, a ...interface{}) Error {
	e := fmt.Errorf(format, a...)
	return new(e, 1, true)
}
