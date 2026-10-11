package app

import (
	"context"
	"errors"
	"fmt"
	"marketplace-api/internal/domain"
	"net/mail"
	"strings"
)

// Объединяет в себе сценарии регистрации, входа, обновления токенов
type AuthService struct {
	repo         UserRepository
	hasher       PasswordHasher
	tokenService TokenService
	IDs          randomIDs
}

type UserDTO struct {
	Email  string
	Role   domain.Role
	Tokens TokenPair
}

func (as AuthService) Register(ctx context.Context, email string, password string, role domain.Role) (UserDTO, error) {
	if err := ctx.Err(); err != nil {
		return UserDTO{}, err
	}

	email = normalizeEmail(email)
	//валидация почты почты
	err := validateEmail(email)
	if err != nil {
		return UserDTO{}, ErrInvalidInput
	}
	if !role.Valid() {
		return UserDTO{}, fmt.Errorf("Invalid role: only %s or %s is acceptable: %w", domain.RoleBuyer, domain.RoleSeller, ErrInvalidInput)
	}
	id := as.IDs.NewID()
	passHash, err := as.hasher.Hash(password)
	if err != nil {
		return UserDTO{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := domain.NewUser(id, email, passHash, role)
	if err != nil {
		return UserDTO{}, ErrInvalidInput
	}
	//Сохранение, внутри проверка на существование пользователя
	err = as.repo.Save(ctx, user)
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

	user, err := as.repo.GetByEmail(ctx, email)
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
	user, err := as.repo.GetByID(ctx, userId)
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
