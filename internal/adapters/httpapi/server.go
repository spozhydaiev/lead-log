package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type ServerConfig struct {
	Address                                string
	ReadTimeout, WriteTimeout, IdleTimeout time.Duration
}

func NewServer(cfg ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: cfg.ReadTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20}
}
func RunServer(ctx context.Context, s *http.Server) error {
	errc := make(chan error, 1)
	go func() { errc <- s.ListenAndServe() }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errc
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
