package dsqlconn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectWithPassword(t *testing.T) {
	t.Run("password auth skips region detection", func(t *testing.T) {
		t.Setenv("POSTGRES_USER", "testuser")
		t.Setenv("POSTGRES_PASSWORD", "testpass")

		// Connect will attempt password auth to a non-existent host,
		// but it should NOT fail on region detection for a non-DSQL endpoint.
		_, err := Connect(context.Background(), ConnectInput{
			Endpoint: "localhost:15432",
			User:     "admin",
		})
		// Expect a connection error (host not listening), not a region detection error.
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "cannot detect region")
		assert.Contains(t, err.Error(), "connecting to localhost:15432")
	})

	t.Run("password auth with default port", func(t *testing.T) {
		t.Setenv("POSTGRES_USER", "testuser")
		t.Setenv("POSTGRES_PASSWORD", "testpass")

		_, err := Connect(context.Background(), ConnectInput{
			Endpoint: "localhost",
			User:     "admin",
		})
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "cannot detect region")
	})

	t.Run("without POSTGRES_USER falls through to IAM auth", func(t *testing.T) {
		// Ensure env vars are not set.
		t.Setenv("POSTGRES_USER", "")

		_, err := Connect(context.Background(), ConnectInput{
			Endpoint: "bad-endpoint",
			User:     "admin",
		})
		// Should fail on region detection (IAM path).
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot detect region")
	})
}

func TestParseRegion(t *testing.T) {
	t.Run("standard endpoint", func(t *testing.T) {
		region, err := ParseRegion("abc123def456.dsql.us-east-1.on.aws")
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", region)
	})

	t.Run("eu-west-1 endpoint", func(t *testing.T) {
		region, err := ParseRegion("cluster-id.dsql.eu-west-1.on.aws")
		require.NoError(t, err)
		assert.Equal(t, "eu-west-1", region)
	})

	t.Run("ap-southeast-2 endpoint", func(t *testing.T) {
		region, err := ParseRegion("my-cluster.dsql.ap-southeast-2.on.aws")
		require.NoError(t, err)
		assert.Equal(t, "ap-southeast-2", region)
	})

	t.Run("invalid endpoint returns error", func(t *testing.T) {
		_, err := ParseRegion("not-a-dsql-endpoint.example.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot detect region")
	})

	t.Run("empty endpoint returns error", func(t *testing.T) {
		_, err := ParseRegion("")
		assert.Error(t, err)
	})

	t.Run("partial match returns error", func(t *testing.T) {
		_, err := ParseRegion("cluster.dsql.on.aws")
		assert.Error(t, err)
	})
}

func TestConnect(t *testing.T) {
	t.Run("validation error on missing endpoint", func(t *testing.T) {
		_, err := Connect(context.Background(), ConnectInput{
			User: "admin",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid connect input")
	})

	t.Run("validation error on missing user", func(t *testing.T) {
		_, err := Connect(context.Background(), ConnectInput{
			Endpoint: "cluster.dsql.us-east-1.on.aws",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid connect input")
	})

	t.Run("invalid endpoint fails region detection", func(t *testing.T) {
		_, err := Connect(context.Background(), ConnectInput{
			Endpoint: "bad-endpoint",
			User:     "admin",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot detect region")
	})
}
