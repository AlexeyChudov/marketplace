package domain

import (
	"strings"

	"github.com/google/uuid"
)

type Role string

const (
	RoleBuyer  Role = "buyer"
	RoleSeller Role = "seller"
)

func (r Role) Valid() bool { return r == RoleBuyer || r == RoleSeller }

// User represents a registered marketplace account. PasswordHash is never a
// plaintext password; password policy and hashing belong outside the domain.
type User struct {
	id           uuid.UUID
	email        string
	passwordHash string
	role         Role
}

func NewUser(id uuid.UUID, email, passwordHash string, role Role) (User, error) {
	if id == uuid.Nil {
		return User{}, ErrInvalidID
	}
	if strings.TrimSpace(email) == "" {
		return User{}, ErrInvalidEmail
	}
	if strings.TrimSpace(passwordHash) == "" {
		return User{}, ErrInvalidPasswordHash
	}
	if !role.Valid() {
		return User{}, ErrInvalidRole
	}

	return User{
		id:           id,
		email:        strings.TrimSpace(email),
		passwordHash: passwordHash,
		role:         role,
	}, nil
}

func (u User) ID() uuid.UUID        { return u.id }
func (u User) Email() string        { return u.email }
func (u User) PasswordHash() string { return u.passwordHash }
func (u User) Role() Role           { return u.role }
