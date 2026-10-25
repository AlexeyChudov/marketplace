package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"marketplace-api/internal/app"
	"marketplace-api/internal/domain"

	"github.com/google/uuid"
)

type contextKey string

const (
	principalKey contextKey = "principal"
	requestIDKey contextKey = "request_id"
)

type Middleware func(http.Handler) http.Handler

func withPrincipal(r *http.Request, principal app.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey, principal))
}

func principalFromContext(r *http.Request) (app.Principal, bool) {
	principal, ok := r.Context().Value(principalKey).(app.Principal)
	return principal, ok
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))
		next.ServeHTTP(w, r)
	})
}

func WithLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writer := &ResponseWriterWithRespStatus{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(writer, r)

			level := slog.LevelInfo
			if writer.statusCode >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.Log(r.Context(), level, "http request",
				"request_id", r.Context().Value(requestIDKey),
				"method", r.Method,
				"path", r.URL.Path,
				"status", writer.statusCode,
				"dur_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

func WithRecover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic while handling request", "request_id", r.Context().Value(requestIDKey), "panic", recovered)
					writeError(w, http.StatusInternalServerError, errInternal, "internal server error", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func WithCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func WithAuth(tokens app.TokenService) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
				return
			}
			claims, err := tokens.Verify(parts[1], app.TokenAccess)
			if err != nil || claims.Type != app.TokenAccess || claims.Subject == uuid.Nil || !claims.Role.Valid() {
				writeError(w, http.StatusUnauthorized, errUnauthenticated, "invalid access token", nil)
				return
			}
			principal := app.Principal{UserID: claims.Subject, Role: claims.Role}
			next.ServeHTTP(w, withPrincipal(r, principal))
		})
	}
}

func WithRequireRole(target domain.Role) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := principalFromContext(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, errUnauthenticated, "authentication required", nil)
				return
			}
			if !target.Valid() || principal.Role != target {
				writeError(w, http.StatusForbidden, errForbidden, fmt.Sprintf("%s role required", target), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
