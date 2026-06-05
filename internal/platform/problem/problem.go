// Package problem is the application error vocabulary. Domain code returns these;
// transport code maps them to ConnectRPC codes via ToConnect.
package problem

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
)

// Kind classifies an application error.
type Kind int

// Error kinds, mapped to ConnectRPC codes by ToConnect.
const (
	KindInternal Kind = iota
	KindNotFound
	KindInvalidInput
	KindPermissionDenied
	KindAlreadyExists
)

// Error is an application error carrying a Kind and a safe, user-facing message.
type Error struct {
	Kind    Kind
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// NotFound builds a not-found error.
func NotFound(format string, args ...any) *Error {
	return &Error{Kind: KindNotFound, Message: fmt.Sprintf(format, args...)}
}

// InvalidInput builds an invalid-argument error.
func InvalidInput(format string, args ...any) *Error {
	return &Error{Kind: KindInvalidInput, Message: fmt.Sprintf(format, args...)}
}

// AlreadyExists builds a conflict error for a resource that already exists.
func AlreadyExists(format string, args ...any) *Error {
	return &Error{Kind: KindAlreadyExists, Message: fmt.Sprintf(format, args...)}
}

// PermissionDenied builds an authorization error. The message is safe to show the
// caller; it must not reveal whether the target resource exists.
func PermissionDenied(format string, args ...any) *Error {
	return &Error{Kind: KindPermissionDenied, Message: fmt.Sprintf(format, args...)}
}

// Internalf wraps cause as an internal error with a safe message.
func Internalf(cause error, format string, args ...any) *Error {
	return &Error{Kind: KindInternal, Message: fmt.Sprintf(format, args...), cause: cause}
}

// ToConnect maps an application error to the appropriate ConnectRPC code.
func ToConnect(err error) error {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) {
		switch pe.Kind {
		case KindNotFound:
			return connect.NewError(connect.CodeNotFound, errors.New(pe.Message))
		case KindInvalidInput:
			return connect.NewError(connect.CodeInvalidArgument, errors.New(pe.Message))
		case KindAlreadyExists:
			return connect.NewError(connect.CodeAlreadyExists, errors.New(pe.Message))
		case KindPermissionDenied:
			return connect.NewError(connect.CodePermissionDenied, errors.New(pe.Message))
		}
	}
	// Don't leak internal details to the client.
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}
