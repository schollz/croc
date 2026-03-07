package pool

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	log "github.com/schollz/logger"
)

// ServerConfig holds configuration for the pool server
type ServerConfig struct {
	Host            string
	Port            int
	TTL             time.Duration
	CleanupInterval time.Duration
}

// RunServer starts the pool server and blocks until shutdown signal is received
func RunServer(config ServerConfig) error {
	log.Infof("Starting pool server on %s:%d", config.Host, config.Port)
	log.Infof("TTL: %v, Cleanup Interval: %v", config.TTL, config.CleanupInterval)

	// Create relay store
	store := NewRelayStore(config.TTL)

	// Start cleanup loop
	stopCh := make(chan struct{})
	go store.StartCleanupLoop(config.CleanupInterval, stopCh)

	// Set up routes using Fiber.
	app := fiber.New(fiber.Config{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	})
	registerPoolRoutes(app, store)

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	stopping := make(chan struct{})

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Infof("Pool server listening on %s", addr)
		if err := app.Listen(addr); err != nil {
			select {
			case <-stopping:
				return
			default:
				serverErrors <- fmt.Errorf("server error: %w", err)
			}
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		close(stopCh)
		return err
	case <-sigCh:
		log.Info("Shutting down pool server...")
		close(stopping)
		close(stopCh)

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := app.ShutdownWithContext(ctx); err != nil {
			log.Errorf("Server shutdown error: %v", err)
			return err
		}

		log.Info("Pool server stopped")
		return nil
	}
}
