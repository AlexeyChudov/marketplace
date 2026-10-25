package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"marketplace-api/internal/app"
	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

type errorCode string

const (
	errValidationFailed errorCode = "validation_failed"
	errUnauthenticated  errorCode = "unauthenticated"
	errForbidden        errorCode = "forbidden"
	errNotFound         errorCode = "not_found"
	errConflict         errorCode = "conflict"
	errInternal         errorCode = "internal"
)

type ErrorResponse struct {
	Err     errorCode    `json:"error"`
	Message string       `json:"message"`
	Fields  []fieldError `json:"fields,omitempty"`
}

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type RegisterInput struct {
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshInput struct {
	RefreshToken string `json:"refresh_token"`
}

type ListingInput struct {
	Title string          `json:"title"`
	Price json.RawMessage `json:"price"`
	Stock *int            `json:"stock"`
}

type PatchListingInput struct {
	Title *string          `json:"title"`
	Price *json.RawMessage `json:"price"`
	Stock *int             `json:"stock"`
}

type BuyInput struct {
	Quantity *int `json:"qty"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type RegisterResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID    uuid.UUID   `json:"id"`
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
}

type ListingResponse struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Price     float64   `json:"price"`
	Stock     int       `json:"stock"`
	SellerID  uuid.UUID `json:"seller_id"`
	CreatedAt string    `json:"created_at"`
}

type PurchaseResponse struct {
	ListingID uuid.UUID `json:"listing_id"`
	Quantity  int       `json:"qty"`
	Remaining int       `json:"remaining"`
}

type Handler struct {
	auth     *app.AuthService
	listings *app.ListingService
	logger   *slog.Logger
}

func NewHandler(auth *app.AuthService, listings *app.ListingService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{auth: auth, listings: listings, logger: logger}
}

type ResponseWriterWithRespStatus struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *ResponseWriterWithRespStatus) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *ResponseWriterWithRespStatus) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

var _ http.ResponseWriter = &ResponseWriterWithRespStatus{}
