package app

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

// Объединяет в себе сценарии регистрации, входа, обновления токенов
type AuthService struct {
	repo         UserRepository
	hasher       PasswordHasher
	tokenService TokenService
	ids          IDGenerator
}

func NewAuthService(repo UserRepository, hasher PasswordHasher, tokenService TokenService, ids IDGenerator) *AuthService {
	if ids == nil {
		ids = randomIDs{}
	}
	return &AuthService{repo: repo, hasher: hasher, tokenService: tokenService, ids: ids}
}

type UserDTO struct {
	ID     uuid.UUID
	Email  string
	Role   domain.Role
	Tokens TokenPair
}

func (as AuthService) Register(ctx context.Context, email string, password string, role domain.Role) (UserDTO, error) {
	if err := ctx.Err(); err != nil {
		return UserDTO{}, err
	}

	email = normalizeEmail(email)
	//валидация почты
	err := validateEmail(email)
	if err != nil {
		return UserDTO{}, ErrInvalidInput
	}
	if password == "" {
		return UserDTO{}, ErrInvalidInput
	}
	if !role.Valid() {
		return UserDTO{}, fmt.Errorf("invalid role: only %s or %s is acceptable: %w", domain.RoleBuyer, domain.RoleSeller, ErrInvalidInput)
	}
	id := as.ids.NewID()
	passHash, err := as.hasher.Hash(password)
	if err != nil {
		return UserDTO{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := domain.NewUser(id, email, passHash, role)
	if err != nil {
		return UserDTO{}, ErrInvalidInput
	}
	//Сохранение, внутри проверка на существование пользователя
	err = as.repo.SaveUser(ctx, user)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return UserDTO{}, fmt.Errorf("%w: %w", ErrConflict, err)
		}
		return UserDTO{}, fmt.Errorf("save user: %w", err)
	}

	tokens, err := as.tokenService.IssuePair(user)
	if err != nil {
		return UserDTO{}, err
	}

	return UserDTO{
		ID:     id,
		Email:  email,
		Role:   role,
		Tokens: tokens,
	}, nil

}

func (as AuthService) Login(ctx context.Context, email string, password string) (TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return TokenPair{}, err
	}

	email = normalizeEmail(email)

	err := validateEmail(email)
	if err != nil {
		return TokenPair{}, ErrInvalidInput
	}

	user, err := as.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return TokenPair{}, ErrUnauthenticated
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load user by email: %w", err)
	}
	err = as.hasher.Compare(user.PasswordHash(), password)
	if errors.Is(err, ErrPasswordMismatch) {
		return TokenPair{}, ErrUnauthenticated
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("compare password: %w", err)
	}
	tokens, err := as.tokenService.IssuePair(user)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue token pair: %w", err)
	}
	return tokens, nil

}

func (as AuthService) Refresh(ctx context.Context, token string) (TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return TokenPair{}, err
	}
	claims, err := as.tokenService.Verify(token, TokenRefresh)
	if err != nil {
		return TokenPair{}, ErrUnauthenticated
	}
	tokenType := claims.Type
	if tokenType != TokenRefresh {
		return TokenPair{}, fmt.Errorf("only refresh is acceptable: %w", ErrUnauthenticated)
	}
	userId := claims.Subject
	user, err := as.repo.GetUserByID(ctx, userId)
	if errors.Is(err, ErrNotFound) {
		return TokenPair{}, ErrUnauthenticated
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load user for refresh: %w", err)
	}
	tokens, err := as.tokenService.IssuePair(user)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue token pair: %w", err)
	}
	return tokens, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return err
	}
	if addr.Address != email {
		return fmt.Errorf("email must contain only an address")
	}
	return nil
}
