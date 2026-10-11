package domain

import "errors"

// Sentinel errors represent violated domain invariants. Application services
// can translate them into use-case errors without coupling the domain to HTTP.
var (
	ErrInvalidID           = errors.New("domain: id must not be zero")
	ErrInvalidEmail        = errors.New("domain: email must not be empty")
	ErrInvalidPasswordHash = errors.New("domain: password hash must not be empty")
	ErrInvalidRole         = errors.New("domain: role must be buyer or seller")
	ErrInvalidTitle        = errors.New("domain: title must not be empty")
	ErrInvalidPrice        = errors.New("domain: price must be positive")
	ErrInvalidStock        = errors.New("domain: stock must not be negative")
	ErrInvalidCreatedAt    = errors.New("domain: created_at must be set")
	ErrInvalidQuantity     = errors.New("domain: quantity must be positive")
	ErrInsufficientStock   = errors.New("domain: insufficient stock")
)
