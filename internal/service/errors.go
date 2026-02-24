package service

import "fmt"

// ErrorKind categorizes service-level errors for handler mapping.
type ErrorKind string

const (
	ErrorBadRequest ErrorKind = "bad_request"
	ErrorNotFound   ErrorKind = "not_found"
	ErrorInternal   ErrorKind = "internal"
)

// Error is a typed service error.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func badRequest(msg string, err error) *Error {
	return &Error{Kind: ErrorBadRequest, Message: msg, Err: err}
}

func notFound(msg string, err error) *Error {
	return &Error{Kind: ErrorNotFound, Message: msg, Err: err}
}

func internal(msg string, err error) *Error {
	return &Error{Kind: ErrorInternal, Message: msg, Err: err}
}
