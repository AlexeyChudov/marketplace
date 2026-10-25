package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"marketplace-api/internal/app"
	"marketplace-api/internal/config"
	"marketplace-api/internal/domain"
)

type Server struct {
	config      config.Config
	logger      *slog.Logger
	authService *app.AuthService
	listings    *app.ListingService
	tokens      app.TokenService
	frontend    []byte
}

func NewServer(cfg config.Config, logger *slog.Logger, auth *app.AuthService, listings *app.ListingService, tokens app.TokenService, frontend []byte) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{config: cfg, logger: logger, authService: auth, listings: listings, tokens: tokens, frontend: frontend}
}

func (s *Server) Handler() http.Handler {
	handler := NewHandler(s.authService, s.listings, s.logger)
	mux := http.NewServeMux()

	common := []Middleware{withRequestID, WithLogger(s.logger), WithRecover(s.logger), WithCORS}
	private := func(endpoint http.Handler, role domain.Role) http.Handler {
		return chain(endpoint, WithAuth(s.tokens), WithRequireRole(role))
	}

	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.frontend)
	}))
	mux.Handle("POST /auth/register", http.HandlerFunc(handler.Register))
	mux.Handle("POST /auth/login", http.HandlerFunc(handler.Login))
	mux.Handle("POST /auth/refresh", http.HandlerFunc(handler.Refresh))
	mux.Handle("GET /listings", http.HandlerFunc(handler.GetListings))
	mux.Handle("GET /listings/{id}", http.HandlerFunc(handler.GetListing))
	mux.Handle("POST /listings", private(http.HandlerFunc(handler.CreateListing), domain.RoleSeller))
	mux.Handle("PATCH /listings/{id}", private(http.HandlerFunc(handler.UpdateListing), domain.RoleSeller))
	mux.Handle("DELETE /listings/{id}", private(http.HandlerFunc(handler.DeleteListing), domain.RoleSeller))
	mux.Handle("POST /listings/{id}/buy", private(http.HandlerFunc(handler.BuyListing), domain.RoleBuyer))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, errNotFound, "route not found", nil)
	}))
	return chain(mux, common...)
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.config.HTTPAddr, err)
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		fmt.Printf("listening on http://127.0.0.1:%d\n", tcpAddr.Port)
	}

	server := &http.Server{
		Addr:              s.config.HTTPAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		err := <-serveResult
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
