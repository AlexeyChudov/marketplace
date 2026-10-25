package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"marketplace-api/internal/app"
	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

// Repository is a concurrency-safe in-memory implementation of the app ports.
type Repository struct {
	mu          sync.RWMutex
	users       map[uuid.UUID]domain.User
	usersByMail map[string]uuid.UUID
	listings    map[uuid.UUID]domain.Listing
}

func NewRepository() *Repository {
	return &Repository{
		users:       make(map[uuid.UUID]domain.User),
		usersByMail: make(map[string]uuid.UUID),
		listings:    make(map[uuid.UUID]domain.Listing),
	}
}

func (r *Repository) SaveUser(ctx context.Context, user domain.User) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.usersByMail[user.Email()]; ok && existingID != user.ID() {
		return app.ErrEmailTaken
	}
	r.users[user.ID()] = user
	r.usersByMail[user.Email()] = user.ID()
	return nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.usersByMail[email]
	if !ok {
		return domain.User{}, app.ErrNotFound
	}
	return r.users[id], nil
}

func (r *Repository) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.users[id]
	if !ok {
		return domain.User{}, app.ErrNotFound
	}
	return user, nil
}

func (r *Repository) Create(ctx context.Context, listing domain.Listing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.listings[listing.ID()]; exists {
		return fmt.Errorf("listing %s already exists", listing.ID())
	}
	r.listings[listing.ID()] = listing
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	if err := ctx.Err(); err != nil {
		return domain.Listing{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	listing, ok := r.listings[id]
	if !ok {
		return domain.Listing{}, app.ErrNotFound
	}
	return listing, nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]domain.Listing, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]domain.Listing, 0, len(r.listings))
	for _, listing := range r.listings {
		all = append(all, listing)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt().Equal(all[j].CreatedAt()) {
			return all[i].ID().String() < all[j].ID().String()
		}
		return all[i].CreatedAt().Before(all[j].CreatedAt())
	})
	total := len(all)
	if offset >= total {
		return []domain.Listing{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (r *Repository) Save(ctx context.Context, listing domain.Listing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.listings[listing.ID()]; !exists {
		return app.ErrNotFound
	}
	r.listings[listing.ID()] = listing
	return nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.listings[id]; !exists {
		return app.ErrNotFound
	}
	delete(r.listings, id)
	return nil
}

func (r *Repository) Purchase(ctx context.Context, id uuid.UUID, quantity int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	listing, ok := r.listings[id]
	if !ok {
		return 0, app.ErrNotFound
	}
	remaining, err := listing.Purchase(quantity)
	if err != nil {
		return remaining, err
	}
	r.listings[id] = listing
	return remaining, nil
}

var _ app.UserRepository = (*Repository)(nil)
var _ app.ListingRepository = (*Repository)(nil)
