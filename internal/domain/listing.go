package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Listing struct {
	id        uuid.UUID
	title     string
	price     int64 // price in the smallest supported currency unit
	stock     int
	sellerID  uuid.UUID
	createdAt time.Time
}

func NewListing(id uuid.UUID, title string, price int64, stock int, sellerID uuid.UUID, createdAt time.Time) (Listing, error) {
	if id == uuid.Nil || sellerID == uuid.Nil {
		return Listing{}, ErrInvalidID
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Listing{}, ErrInvalidTitle
	}
	if price <= 0 {
		return Listing{}, ErrInvalidPrice
	}
	if stock < 0 {
		return Listing{}, ErrInvalidStock
	}
	if createdAt.IsZero() {
		return Listing{}, ErrInvalidCreatedAt
	}

	return Listing{id: id, title: title, price: price, stock: stock, sellerID: sellerID, createdAt: createdAt}, nil
}

// Update validates all replacement values before changing any field. Ownership
// must be checked by the application use case before calling this method.
func (l *Listing) Update(title string, price int64, stock int) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return ErrInvalidTitle
	}
	if price <= 0 {
		return ErrInvalidPrice
	}
	if stock < 0 {
		return ErrInvalidStock
	}

	l.title, l.price, l.stock = title, price, stock
	return nil
}

// Purchase decreases stock if enough units remain. Concurrent callers still
// need synchronization around the read-modify-write in the storage adapter.
func (l *Listing) Purchase(quantity int) (remaining int, err error) {
	if quantity <= 0 {
		return l.stock, ErrInvalidQuantity
	}
	if quantity > l.stock {
		return l.stock, ErrInsufficientStock
	}
	l.stock -= quantity
	return l.stock, nil
}

func (l Listing) ID() uuid.UUID        { return l.id }
func (l Listing) Title() string        { return l.title }
func (l Listing) Price() int64         { return l.price }
func (l Listing) Stock() int           { return l.stock }
func (l Listing) SellerID() uuid.UUID  { return l.sellerID }
func (l Listing) CreatedAt() time.Time { return l.createdAt }
