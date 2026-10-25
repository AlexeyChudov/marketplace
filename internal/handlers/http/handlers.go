package http

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"marketplace-api/internal/app"
	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

const maxRequestBody = 1 << 20

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}

	result, err := h.auth.Register(r.Context(), in.Email, in.Password, in.Role)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, RegisterResponse{
		AccessToken:  result.Tokens.AccessToken,
		RefreshToken: result.Tokens.RefreshToken,
		User: UserResponse{
			ID:    result.ID,
			Email: result.Email,
			Role:  result.Role,
		},
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}
	tokens, err := h.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TokenResponse{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var in RefreshInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}
	tokens, err := h.auth.Refresh(r.Context(), in.RefreshToken)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TokenResponse{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken})
}

func (h *Handler) GetListings(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	var fields []fieldError
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			fields = append(fields, fieldError{Field: "limit", Message: "must be an integer"})
		} else {
			limit = value
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			fields = append(fields, fieldError{Field: "offset", Message: "must be integer"})
		} else {
			offset = value
		}
	}
	if len(fields) != 0 {
		writeValidationError(w, fields)
		return
	}

	listings, total, err := h.listings.List(r.Context(), limit, offset)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	response := make([]ListingResponse, 0, len(listings))
	for _, listing := range listings {
		response = append(response, listingResponse(listing))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetListing(w http.ResponseWriter, r *http.Request) {
	id, err := parseListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "listing id is invalid", []fieldError{{Field: "id", Message: "must be a UUID"}})
		return
	}
	listing, err := h.listings.Get(r.Context(), id)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listingResponse(listing))
}

func (h *Handler) CreateListing(w http.ResponseWriter, r *http.Request) {
	var in ListingInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}
	price, priceErr := parsePrice(in.Price)
	var fields []fieldError
	if strings.TrimSpace(in.Title) == "" {
		fields = append(fields, fieldError{Field: "title", Message: "must not be empty"})
	}
	if priceErr != nil {
		fields = append(fields, fieldError{Field: "price", Message: "must be positive"})
	}
	if in.Stock == nil {
		fields = append(fields, fieldError{Field: "stock", Message: "is required"})
	} else if *in.Stock < 0 {
		fields = append(fields, fieldError{Field: "stock", Message: "must not be negative"})
	}
	if len(fields) != 0 {
		writeValidationError(w, fields)
		return
	}
	actor, ok := principalFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
		return
	}
	listing, err := h.listings.Create(r.Context(), actor, app.CreateListingInput{Title: in.Title, Price: price, Stock: *in.Stock})
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, listingResponse(listing))
}

func (h *Handler) UpdateListing(w http.ResponseWriter, r *http.Request) {
	id, err := parseListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "listing id is invalid", []fieldError{{Field: "id", Message: "must be a UUID"}})
		return
	}
	var in PatchListingInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}
	var fields []fieldError
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		fields = append(fields, fieldError{Field: "title", Message: "must not be empty"})
	}
	var price *int64
	if in.Price != nil {
		value, err := parsePrice(*in.Price)
		if err != nil {
			fields = append(fields, fieldError{Field: "price", Message: "must be positive"})
		} else {
			price = &value
		}
	}
	if in.Stock != nil && *in.Stock < 0 {
		fields = append(fields, fieldError{Field: "stock", Message: "must not be negative"})
	}
	if len(fields) != 0 {
		writeValidationError(w, fields)
		return
	}
	actor, ok := principalFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
		return
	}
	listing, err := h.listings.Update(r.Context(), actor, id, app.UpdateListingInput{Title: in.Title, Price: price, Stock: in.Stock})
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listingResponse(listing))
}

func (h *Handler) DeleteListing(w http.ResponseWriter, r *http.Request) {
	id, err := parseListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "listing id is invalid", []fieldError{{Field: "id", Message: "must be a UUID"}})
		return
	}
	actor, ok := principalFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
		return
	}
	if err := h.listings.Delete(r.Context(), actor, id); err != nil {
		h.writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) BuyListing(w http.ResponseWriter, r *http.Request) {
	id, err := parseListingID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "listing id is invalid", []fieldError{{Field: "id", Message: "must be a UUID"}})
		return
	}
	var in BuyInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", nil)
		return
	}
	if in.Quantity == nil || *in.Quantity <= 0 {
		writeValidationError(w, []fieldError{{Field: "qty", Message: "must be a positive integer"}})
		return
	}
	actor, ok := principalFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
		return
	}
	result, err := h.listings.Purchase(r.Context(), actor, id, *in.Quantity)
	if err != nil {
		h.writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PurchaseResponse{ListingID: result.ListingID, Quantity: result.Quantity, Remaining: result.Remaining})
}

func (h *Handler) writeAppError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, errValidationFailed, "request is invalid", nil)
	case errors.Is(err, app.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
	case errors.Is(err, app.ErrForbidden):
		writeError(w, http.StatusForbidden, errForbidden, "access denied", nil)
	case errors.Is(err, app.ErrNotFound):
		writeError(w, http.StatusNotFound, errNotFound, "resource not found", nil)
	case errors.Is(err, app.ErrConflict), errors.Is(err, domain.ErrInsufficientStock):
		writeError(w, http.StatusConflict, errConflict, "conflict", nil)
	default:
		h.logger.Error("application handler failed", "error", err)
		writeError(w, http.StatusInternalServerError, errInternal, "internal server error", nil)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal","message":"internal server error"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, code errorCode, message string, fields []fieldError) {
	writeJSON(w, status, ErrorResponse{Err: code, Message: message, Fields: fields})
}

func writeValidationError(w http.ResponseWriter, fields []fieldError) {
	writeError(w, http.StatusBadRequest, errValidationFailed, "request body is invalid", fields)
}

func parseListingID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(r.PathValue("id"))
}

func parsePrice(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return 0, errors.New("price is required")
	}
	value, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > float64(math.MaxInt64)/100 {
		return 0, errors.New("price must be positive and finite")
	}
	minorUnits := math.Round(value * 100)
	if minorUnits < 1 || minorUnits > float64(math.MaxInt64) {
		return 0, errors.New("price is outside supported range")
	}
	return int64(minorUnits), nil
}

func listingResponse(listing domain.Listing) ListingResponse {
	return ListingResponse{
		ID: listing.ID(), Title: listing.Title(), Price: float64(listing.Price()) / 100,
		Stock: listing.Stock(), SellerID: listing.SellerID(), CreatedAt: listing.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
}
