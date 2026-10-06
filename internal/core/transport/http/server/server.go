package core_http_server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	core_logger "github.com/vasya2314/golang-kp/internal/core/logger"
	"go.uber.org/zap"
)

type HTTPServer struct {
	*chi.Mux
	config Config
	logger *core_logger.Logger
}

func NewHTTPServer(config Config, logger *core_logger.Logger) *HTTPServer {
	return &HTTPServer{
		Mux:    chi.NewRouter(),
		config: config,
		logger: logger,
	}
}

func (s *HTTPServer) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:    s.config.Address,
		Handler: s.Mux,
	}

	ch := make(chan error, 1)

	go func() {
		defer close(ch)

		s.logger.Warn("Starting HTTP server", zap.String("address", s.config.Address))

		err := server.ListenAndServe()

		if !errors.Is(err, http.ErrServerClosed) {
			ch <- err
		}
	}()

	select {
	case err := <-ch:
		if err != nil {
			return fmt.Errorf("запуск HTTP-сервера: %w", err)
		}
	case <-ctx.Done():
		s.logger.Warn("Shutting down HTTP server...")

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			s.config.ShutdownTimeout,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()

			return fmt.Errorf("остановка HTTP-сервера: %w", err)
		}

		s.logger.Warn("HTTP server stopped")
	}

	return nil
}
