package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

func (runtime *Runtime) Serve(ctx context.Context, address string) error {
	address = firstNonEmpty(address, runtime.settings.ListenAddress)
	if err := runtime.ensureTopics(ctx); err != nil {
		return err
	}
	authenticator, err := runtime.webAuthenticator()
	if err != nil {
		return err
	}
	handler, err := web.New(WebAdapter{Store: runtime.store}, web.Options{
		Logger:           runtime.logger,
		Now:              runtime.now,
		Location:         domain.ShanghaiLocation(),
		SiteName:         "RepoTempo",
		Locale:           runtime.settings.Locale,
		Watcher:          runtime,
		AllowLocalWrites: isLoopbackListenAddress(address),
		WriteToken:       runtime.settings.WebWriteToken,
		Auth:             authenticator,
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
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for Web server: %w", err)
	}
	if err := runtime.startImportWorker(ctx); err != nil {
		_ = listener.Close()
		return err
	}
	defer runtime.stopImportWorker()
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(listener)
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

func isLoopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
