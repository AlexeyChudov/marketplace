package app

import (
	"context"
	"time"

	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

// UserRepository is the persistence port needed by authentication use cases.
// Save returns ErrEmailTaken when the normalized email violates uniqueness.
// GetByEmail returns ErrNotFound when user with provided email wasn't found
type UserRepository interface {
	Save(ctx context.Context, user domain.User) error
	GetByEmail(ctx context.Context, normalizedEmail string) (domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

// ListingRepository owns persistence and atomic stock changes. Purchase must
// read, validate, and decrement stock in one critical section/transaction.
type ListingRepository interface {
	Create(ctx context.Context, listing domain.Listing) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Listing, error)
	List(ctx context.Context, limit, offset int) ([]domain.Listing, int, error)
	Save(ctx context.Context, listing domain.Listing) error
	Delete(ctx context.Context, id uuid.UUID) error
	Purchase(ctx context.Context, id uuid.UUID, quantity int) (remaining int, err error)
}

type PasswordHasher interface {
	Hash(plainText string) (string, error)
	Compare(hash, plainText string) error
}

type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

type TokenClaims struct {
	Subject uuid.UUID
	Type    TokenType
}

// TokenService verifies signature, expiration, and token type in Verify.
type TokenService interface {
	IssuePair(user domain.User) (TokenPair, error)
	Verify(raw string, expectedType TokenType) (TokenClaims, error)
}

type IDGenerator interface {
	NewID() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type randomIDs struct{}

func (randomIDs) NewID() uuid.UUID { return uuid.New() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
