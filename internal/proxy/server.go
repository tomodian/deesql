package proxy

import (
	"context"
	"net"
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
		go handleConnection(ctx, conn, in.UpstreamAddr)
	}
}
