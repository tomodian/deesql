package proxy

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-playground/validator/v10"

	"tomodian/deesql/internal/ui"
)

// RunInput holds the configuration for the proxy server.
type RunInput struct {
	ListenAddr   string `validate:"required"`
	UpstreamAddr string `validate:"required"`
}

// Run starts the DSQL-filtering proxy server.
func Run(ctx context.Context, in RunInput) error {
	if err := validator.New().Struct(in); err != nil {
		return err
	}

	// Detect bypass mode from environment variables.
	// When POSTGRES_USER is set, the proxy accepts any client auth and
	// connects to the backend using the env-var credentials.
	var bypass *BypassConfig
	if pgUser := os.Getenv("POSTGRES_USER"); pgUser != "" {
		bypass = &BypassConfig{
			User:     pgUser,
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Database: os.Getenv("POSTGRES_DB"),
		}
		ui.Info("Auth bypass enabled (POSTGRES_USER=%s)", pgUser)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", in.ListenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	ui.Info("Proxy listening on %s, forwarding to %s", in.ListenAddr, in.UpstreamAddr)
	ui.Info("Press Ctrl+C to stop")

	// Close the listener when context is cancelled so Accept() unblocks.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// If context was cancelled, shut down gracefully.
			select {
			case <-ctx.Done():
				ui.Info("Proxy shutting down")
				return nil
			default:
				ui.Error("Accept error: %v", err)
				continue
			}
		}
		go handleConnection(ctx, conn, in.UpstreamAddr, bypass)
	}
}
