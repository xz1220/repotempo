package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/web"
)

func (runtime *Runtime) Serve(ctx context.Context, address string) error {
	address = firstNonEmpty(address, runtime.settings.ListenAddress)
	handler, err := web.New(WebAdapter{Store: runtime.store}, web.Options{
		Logger:   runtime.logger,
		Now:      runtime.now,
		Location: domain.ShanghaiLocation(),
		SiteName: "GitHub Radar",
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	result := make(chan error, 1)
	go func() {
		result <- server.ListenAndServe()
	}()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve %s: %w", address, err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down Web server: %w", err)
		}
		err := <-result
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve %s: %w", address, err)
		}
		return nil
	}
}
