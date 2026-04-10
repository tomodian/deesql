package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	t.Run("validation error on empty input", func(t *testing.T) {
		err := Run(context.Background(), RunInput{})
		assert.Error(t, err)
	})

	t.Run("validation error missing upstream", func(t *testing.T) {
		err := Run(context.Background(), RunInput{ListenAddr: ":0"})
		assert.Error(t, err)
	})

	t.Run("listen error on invalid address", func(t *testing.T) {
		err := Run(context.Background(), RunInput{
			ListenAddr:   "invalid-address-not-parseable::::::",
			UpstreamAddr: "localhost:5432",
		})
		assert.Error(t, err)
	})

	t.Run("starts and stops on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, RunInput{
				ListenAddr:   "127.0.0.1:0",
				UpstreamAddr: "127.0.0.1:5432",
			})
		}()

		// Give it a moment to start listening.
		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after context cancellation")
		}
	})
}
