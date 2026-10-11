package app

import (
	"context"
	"errors"
	"fmt"

	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

type Principal struct {
	UserID uuid.UUID
	Role   domain.Role
}

type ListingService struct {
	listings ListingRepository
	ids      IDGenerator
	clock    Clock
}

func NewListingService(listings ListingRepository, ids IDGenerator, clock Clock) *ListingService {
	if ids == nil {
		ids = randomIDs{}
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &ListingService{listings: listings, ids: ids, clock: clock}
}

type CreateListingInput struct {
	Title string
	Price int64
	Stock int
}

type UpdateListingInput struct {
	Title *string
	Price *int64
	Stock *int
}

type PurchaseResult struct {
	ListingID uuid.UUID
	Quantity  int
	Remaining int
}

func (s *ListingService) Create(ctx context.Context, actor Principal, in CreateListingInput) (domain.Listing, error) {
	if err := ctx.Err(); err != nil {
		return domain.Listing{}, err
	}
	if err := requireRole(actor, domain.RoleSeller); err != nil {
		return domain.Listing{}, err
	}
	listing, err := domain.NewListing(s.ids.NewID(), in.Title, in.Price, in.Stock, actor.UserID, s.clock.Now())
	if err != nil {
		return domain.Listing{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if err := s.listings.Create(ctx, listing); err != nil {
		return domain.Listing{}, fmt.Errorf("create listing: %w", err)
	}
	return listing, nil
}

func (s *ListingService) Get(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	if err := ctx.Err(); err != nil {
		return domain.Listing{}, err
	}
	listing, err := s.listings.GetByID(ctx, id)
	if err != nil {
		return domain.Listing{}, mapNotFound(err)
	}
	return listing, nil
}

func (s *ListingService) List(ctx context.Context, limit, offset int) ([]domain.Listing, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("%w: limit must be 1..100 and offset non-negative", ErrInvalidInput)
	}
	items, total, err := s.listings.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list listings: %w", err)
	}
	return items, total, nil
}

func (s *ListingService) Update(ctx context.Context, actor Principal, id uuid.UUID, in UpdateListingInput) (domain.Listing, error) {
	if err := ctx.Err(); err != nil {
		return domain.Listing{}, err
	}
	if err := requireRole(actor, domain.RoleSeller); err != nil {
		return domain.Listing{}, err
	}
	if in.Title == nil && in.Price == nil && in.Stock == nil {
		return domain.Listing{}, fmt.Errorf("%w: at least one field must be provided", ErrInvalidInput)
	}
	listing, err := s.listings.GetByID(ctx, id)
	if err != nil {
		return domain.Listing{}, mapNotFound(err)
	}
	if listing.SellerID() != actor.UserID {
		return domain.Listing{}, ErrForbidden
	}

	title, price, stock := listing.Title(), listing.Price(), listing.Stock()
	if in.Title != nil {
		title = *in.Title
	}
	if in.Price != nil {
		price = *in.Price
	}
	if in.Stock != nil {
		stock = *in.Stock
	}
	if err := listing.Update(title, price, stock); err != nil {
		return domain.Listing{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if err := s.listings.Save(ctx, listing); err != nil {
		return domain.Listing{}, fmt.Errorf("update listing: %w", err)
	}
	return listing, nil
}

func (s *ListingService) Delete(ctx context.Context, actor Principal, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireRole(actor, domain.RoleSeller); err != nil {
		return err
	}
	listing, err := s.listings.GetByID(ctx, id)
	if err != nil {
		return mapNotFound(err)
	}
	if listing.SellerID() != actor.UserID {
		return ErrForbidden
	}
	if err := s.listings.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete listing: %w", err)
	}
	return nil
}

func (s *ListingService) Purchase(ctx context.Context, actor Principal, id uuid.UUID, quantity int) (PurchaseResult, error) {
	if err := ctx.Err(); err != nil {
		return PurchaseResult{}, err
	}
	if err := requireRole(actor, domain.RoleBuyer); err != nil {
		return PurchaseResult{}, err
	}
	if quantity <= 0 {
		return PurchaseResult{}, fmt.Errorf("%w: %w", ErrInvalidInput, domain.ErrInvalidQuantity)
	}
	remaining, err := s.listings.Purchase(ctx, id, quantity)
	if err != nil {
		if errors.Is(err, domain.ErrInsufficientStock) {
			return PurchaseResult{}, fmt.Errorf("%w: %w", ErrConflict, err)
		}
		if errors.Is(err, ErrNotFound) {
			return PurchaseResult{}, ErrNotFound
		}
		return PurchaseResult{}, fmt.Errorf("purchase listing: %w", err)
	}
	return PurchaseResult{ListingID: id, Quantity: quantity, Remaining: remaining}, nil
}

func requireRole(actor Principal, want domain.Role) error {
	if actor.UserID == uuid.Nil {
		return ErrUnauthenticated
	}
	if !actor.Role.Valid() || actor.Role != want {
		return ErrForbidden
	}
	return nil
}

func mapNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("load listing: %w", err)
}
