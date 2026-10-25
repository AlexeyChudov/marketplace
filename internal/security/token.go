package security

import (
	"errors"
	"time"

	"marketplace-api/internal/app"
	"marketplace-api/internal/domain"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	accessTTL  = 5 * time.Minute
	refreshTTL = 7 * 24 * time.Hour
)

var ErrInvalidToken = errors.New("invalid token")

type TokenService struct {
	secret []byte
}

type claims struct {
	Role domain.Role   `json:"role"`
	Type app.TokenType `json:"typ"`
	jwt.RegisteredClaims
}

func NewTokenService(secret string) (*TokenService, error) {
	if secret == "" {
		return nil, errors.New("JWT secret is required")
	}
	return &TokenService{secret: []byte(secret)}, nil
}

func (s *TokenService) IssuePair(user domain.User) (app.TokenPair, error) {
	now := time.Now().UTC()
	access, err := s.issue(user, app.TokenAccess, now, now.Add(accessTTL))
	if err != nil {
		return app.TokenPair{}, err
	}
	refresh, err := s.issue(user, app.TokenRefresh, now, now.Add(refreshTTL))
	if err != nil {
		return app.TokenPair{}, err
	}
	return app.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *TokenService) issue(user domain.User, tokenType app.TokenType, issuedAt, expiresAt time.Time) (string, error) {
	payload := claims{
		Role: user.Role(),
		Type: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID().String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, payload).SignedString(s.secret)
}

func (s *TokenService) Verify(raw string, expectedType app.TokenType) (app.TokenClaims, error) {
	payload := &claims{}
	parsed, err := jwt.ParseWithClaims(raw, payload, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || parsed == nil || !parsed.Valid {
		return app.TokenClaims{}, ErrInvalidToken
	}
	if payload.ExpiresAt == nil || payload.IssuedAt == nil ||
		payload.Type != expectedType || !payload.Role.Valid() {
		return app.TokenClaims{}, ErrInvalidToken
	}
	userID, err := uuid.Parse(payload.Subject)
	if err != nil || userID == uuid.Nil {
		return app.TokenClaims{}, ErrInvalidToken
	}
	return app.TokenClaims{
		Subject:   userID,
		Role:      payload.Role,
		Type:      payload.Type,
		IssuedAt:  payload.IssuedAt.Time,
		ExpiresAt: payload.ExpiresAt.Time,
	}, nil
}

var _ app.TokenService = (*TokenService)(nil)
