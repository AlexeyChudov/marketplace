package app

import "errors"

// Application errors describe use-case outcomes. The HTTP adapter maps these
// to transport status codes; repositories should return these sentinels for
// not-found and unique-email failures.
var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrConflict         = errors.New("conflict")
	ErrEmailTaken       = errors.New("email already registered")
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrForbidden        = errors.New("forbidden")
	ErrNotFound         = errors.New("not found")
	ErrPasswordMismatch = errors.New("password does not match")
)
