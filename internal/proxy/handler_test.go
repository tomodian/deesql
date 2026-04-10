package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgproto3/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failWriter is an io.Writer that always returns an error.
type failWriter struct{}

func newLastSQL() *atomic.Value {
	v := &atomic.Value{}
	v.Store("")
	return v
}

func (f *failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// buildStartupMessage creates raw bytes for a PG StartupMessage.
func buildStartupMessage(user, database string) []byte {
	var buf bytes.Buffer
	// Placeholder for length (4 bytes).
	buf.Write([]byte{0, 0, 0, 0})
	// Protocol version 3.0.
	binary.Write(&buf, binary.BigEndian, uint32(196608))
	buf.WriteString("user")
	buf.WriteByte(0)
	buf.WriteString(user)
	buf.WriteByte(0)
	buf.WriteString("database")
	buf.WriteByte(0)
	buf.WriteString(database)
	buf.WriteByte(0)
	buf.WriteByte(0) // terminal null
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	return b
}

// buildSSLRequest creates raw bytes for an SSLRequest message.
func buildSSLRequest() []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:4], 8)
	binary.BigEndian.PutUint32(buf[4:8], sslRequestCode)
	return buf
}

// buildCancelRequest creates raw bytes for a CancelRequest message.
func buildCancelRequest(pid, key uint32) []byte {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:4], 16)
	binary.BigEndian.PutUint32(buf[4:8], cancelRequestCode)
	binary.BigEndian.PutUint32(buf[8:12], pid)
	binary.BigEndian.PutUint32(buf[12:16], key)
	return buf
}

// mockBackend starts a TCP listener that accepts one connection and runs handler.
// Returns the listener address and a channel to receive the accepted connection.
func mockBackend(t *testing.T, handler func(net.Conn)) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		handler(conn)
	}()
	return ln
}

// pgBackendHandler simulates a PostgreSQL backend that:
// 1. Reads the startup message
// 2. Sends AuthenticationOk + ReadyForQuery
// 3. For each Query received, sends CommandComplete + ReadyForQuery
// 4. Returns when connection closes
func pgBackendHandler(t *testing.T) func(net.Conn) {
	t.Helper()
	return func(conn net.Conn) {
		defer conn.Close()
		// Read startup message.
		_, _, err := readStartupMessage(conn)
		if err != nil {
			return
		}

		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(conn), conn)
		_ = frontend // we use raw writes for backend responses

		// Send AuthenticationOk.
		authOk := &pgproto3.AuthenticationOk{}
		buf, _ := authOk.Encode(nil)
		conn.Write(buf)

		// Send ReadyForQuery.
		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		buf, _ = rfq.Encode(nil)
		conn.Write(buf)

		// Message loop: read frontend messages and respond.
		backend := pgproto3.NewBackend(pgproto3.NewChunkReader(conn), conn)
		for {
			msg, err := backend.Receive()
			if err != nil {
				return
			}
			switch msg.(type) {
			case *pgproto3.Query:
				cc := &pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}
				buf, _ = cc.Encode(nil)
				conn.Write(buf)
				rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
				buf, _ = rfq.Encode(nil)
				conn.Write(buf)
			case *pgproto3.Terminate:
				return
			}
		}
	}
}

// pgPasswordBackendHandler simulates a PostgreSQL backend with password auth:
// 1. Reads startup, sends AuthenticationCleartextPassword
// 2. Reads PasswordMessage -- if not received (bypass), sends AuthenticationOk anyway (trust fallback)
// 3. Sends AuthenticationOk + ReadyForQuery
// 4. Handles Query/Terminate
func pgPasswordBackendHandler(t *testing.T) func(net.Conn) {
	t.Helper()
	return func(conn net.Conn) {
		defer conn.Close()
		_, _, err := readStartupMessage(conn)
		if err != nil {
			return
		}

		// Ask for password.
		authReq := &pgproto3.AuthenticationCleartextPassword{}
		buf, _ := authReq.Encode(nil)
		conn.Write(buf)

		// In bypass mode, the proxy won't forward the password.
		// The backend still sends AuthOk (simulating trust auth fallback).
		authOk := &pgproto3.AuthenticationOk{}
		buf, _ = authOk.Encode(nil)
		conn.Write(buf)

		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		buf, _ = rfq.Encode(nil)
		conn.Write(buf)

		backend := pgproto3.NewBackend(pgproto3.NewChunkReader(conn), conn)
		for {
			msg, err := backend.Receive()
			if err != nil {
				return
			}
			switch msg.(type) {
			case *pgproto3.Query:
				cc := &pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}
				buf, _ = cc.Encode(nil)
				conn.Write(buf)
				rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
				buf, _ = rfq.Encode(nil)
				conn.Write(buf)
			case *pgproto3.Terminate:
				return
			}
		}
	}
}

func TestReadStartupMessage(t *testing.T) {
	t.Run("valid startup message", func(t *testing.T) {
		msg := buildStartupMessage("postgres", "postgres")
		r := bytes.NewReader(msg)
		raw, code, err := readStartupMessage(r)
		require.NoError(t, err)
		assert.Equal(t, uint32(196608), code) // protocol 3.0
		assert.Equal(t, msg, raw)
	})

	t.Run("SSL request", func(t *testing.T) {
		msg := buildSSLRequest()
		r := bytes.NewReader(msg)
		raw, code, err := readStartupMessage(r)
		require.NoError(t, err)
		assert.Equal(t, uint32(sslRequestCode), code)
		assert.Equal(t, msg, raw)
	})

	t.Run("cancel request", func(t *testing.T) {
		msg := buildCancelRequest(42, 99)
		r := bytes.NewReader(msg)
		raw, code, err := readStartupMessage(r)
		require.NoError(t, err)
		assert.Equal(t, uint32(cancelRequestCode), code)
		assert.Equal(t, msg, raw)
	})

	t.Run("empty reader returns error", func(t *testing.T) {
		r := bytes.NewReader(nil)
		_, _, err := readStartupMessage(r)
		assert.Error(t, err)
	})

	t.Run("truncated message returns error", func(t *testing.T) {
		// Write length indicating 20 bytes but only provide 8.
		buf := make([]byte, 8)
		binary.BigEndian.PutUint32(buf[0:4], 20)
		binary.BigEndian.PutUint32(buf[4:8], 196608)
		r := bytes.NewReader(buf)
		_, _, err := readStartupMessage(r)
		assert.Error(t, err)
	})
}

// dummyBackend creates a pgproto3.Backend from a buffer (for reading client messages).
func TestRelayAuth(t *testing.T) {
	t.Run("relays auth and stops at ReadyForQuery", func(t *testing.T) {
		var backendBuf bytes.Buffer
		authOk := &pgproto3.AuthenticationOk{}
		b, _ := authOk.Encode(nil)
		backendBuf.Write(b)

		ps := &pgproto3.ParameterStatus{Name: "server_version", Value: "15.0"}
		b, _ = ps.Encode(nil)
		backendBuf.Write(b)

		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		b, _ = rfq.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&backendBuf),
			io.Discard,
		)

		var clientBuf bytes.Buffer
		err := relayAuth(frontend, &clientBuf)
		require.NoError(t, err)
		assert.True(t, clientBuf.Len() > 0, "expected data written to client")
	})

	t.Run("returns error on closed reader", func(t *testing.T) {
		var empty bytes.Buffer
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&empty),
			io.Discard,
		)
		err := relayAuth(frontend, io.Discard)
		assert.Error(t, err)
	})

	t.Run("returns error on write failure", func(t *testing.T) {
		var backendBuf bytes.Buffer
		authOk := &pgproto3.AuthenticationOk{}
		b, _ := authOk.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&backendBuf),
			io.Discard,
		)
		err := relayAuth(frontend, &failWriter{})
		assert.Error(t, err)
	})
}

func TestBypassAuth(t *testing.T) {
	t.Run("trust auth backend", func(t *testing.T) {
		// Backend: AuthenticationOk + ParameterStatus + ReadyForQuery.
		var backendBuf bytes.Buffer
		authOk := &pgproto3.AuthenticationOk{}
		b, _ := authOk.Encode(nil)
		backendBuf.Write(b)

		ps := &pgproto3.ParameterStatus{Name: "server_version", Value: "15.0"}
		b, _ = ps.Encode(nil)
		backendBuf.Write(b)

		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		b, _ = rfq.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(&backendBuf), io.Discard)

		var clientOut bytes.Buffer
		err := bypassAuth(frontend, &clientOut, io.Discard, "")
		require.NoError(t, err)

		// Client should receive: AuthOk, ParameterStatus, ReadyForQuery.
		clientFrontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(&clientOut), io.Discard)

		msg, err := clientFrontend.Receive()
		require.NoError(t, err)
		_, ok := msg.(*pgproto3.AuthenticationOk)
		require.True(t, ok, "expected AuthenticationOk, got %T", msg)

		msg, err = clientFrontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ParameterStatus)
		require.True(t, ok, "expected ParameterStatus, got %T", msg)

		msg, err = clientFrontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok, "expected ReadyForQuery, got %T", msg)
	})

	t.Run("password auth backend sends password", func(t *testing.T) {
		// Backend: AuthenticationCleartextPassword, then AuthenticationOk + ReadyForQuery.
		var backendBuf bytes.Buffer
		authReq := &pgproto3.AuthenticationCleartextPassword{}
		b, _ := authReq.Encode(nil)
		backendBuf.Write(b)

		authOk := &pgproto3.AuthenticationOk{}
		b, _ = authOk.Encode(nil)
		backendBuf.Write(b)

		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		b, _ = rfq.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(&backendBuf), io.Discard)

		var clientOut, backendOut bytes.Buffer
		err := bypassAuth(frontend, &clientOut, &backendOut, "mypassword")
		require.NoError(t, err)

		// Backend should have received the bypass password.
		assert.True(t, backendOut.Len() > 0, "expected password sent to backend")
	})

	t.Run("backend auth error forwarded to client", func(t *testing.T) {
		// Backend: AuthenticationCleartextPassword, then ErrorResponse.
		var backendBuf bytes.Buffer
		authReq := &pgproto3.AuthenticationCleartextPassword{}
		b, _ := authReq.Encode(nil)
		backendBuf.Write(b)

		errResp := &pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "password authentication failed"}
		b, _ = errResp.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(&backendBuf), io.Discard)

		var clientOut bytes.Buffer
		err := bypassAuth(frontend, &clientOut, io.Discard, "wrongpass")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backend auth failed")
	})
}

func TestBuildStartupBytes(t *testing.T) {
	t.Run("with user and database", func(t *testing.T) {
		raw := buildStartupBytes("myuser", "mydb")
		r := bytes.NewReader(raw)
		_, code, err := readStartupMessage(r)
		require.NoError(t, err)
		assert.Equal(t, uint32(196608), code) // protocol 3.0

		// Verify the params are in the message.
		assert.Contains(t, string(raw), "user")
		assert.Contains(t, string(raw), "myuser")
		assert.Contains(t, string(raw), "database")
		assert.Contains(t, string(raw), "mydb")
	})

	t.Run("with user only", func(t *testing.T) {
		raw := buildStartupBytes("pguser", "")
		assert.Contains(t, string(raw), "pguser")
		assert.NotContains(t, string(raw), "database")
	})
}

func TestHandleConnectionBypass(t *testing.T) {
	t.Run("bypass mode with trust auth backend", func(t *testing.T) {
		ln := mockBackend(t, pgBackendHandler(t))
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		bypass := &BypassConfig{User: "postgres", Database: "postgres"}

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), bypass)
			close(done)
		}()

		// Send startup message (bypass will replace it for backend).
		clientConn.Write(buildStartupMessage("dsqluser", "dsqldb"))

		// Client should get AuthenticationOk immediately (bypass).
		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(clientConn), clientConn)
		msg, err := frontend.Receive()
		require.NoError(t, err)
		_, ok := msg.(*pgproto3.AuthenticationOk)
		require.True(t, ok, "expected AuthenticationOk, got %T", msg)

		// Then ReadyForQuery.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok, "expected ReadyForQuery, got %T", msg)

		// Send a query -- should still work.
		q := &pgproto3.Query{String: "SELECT 1"}
		buf, _ := q.Encode(nil)
		clientConn.Write(buf)

		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.CommandComplete)
		require.True(t, ok, "expected CommandComplete, got %T", msg)

		// Terminate.
		term := &pgproto3.Terminate{}
		buf, _ = term.Encode(nil)
		clientConn.Write(buf)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return")
		}
	})

	t.Run("bypass mode with password auth backend", func(t *testing.T) {
		ln := mockBackend(t, pgPasswordBackendHandler(t))
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		bypass := &BypassConfig{User: "postgres", Password: "secret", Database: "postgres"}

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), bypass)
			close(done)
		}()

		clientConn.Write(buildStartupMessage("dsqluser", "dsqldb"))

		frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(clientConn), clientConn)

		// AuthenticationOk (bypass).
		msg, err := frontend.Receive()
		require.NoError(t, err)
		_, ok := msg.(*pgproto3.AuthenticationOk)
		require.True(t, ok, "expected AuthenticationOk, got %T", msg)

		// ReadyForQuery.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok, "expected ReadyForQuery, got %T", msg)

		// Terminate.
		term := &pgproto3.Terminate{}
		buf, _ := term.Encode(nil)
		clientConn.Write(buf)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return")
		}
	})
}

func TestSendError(t *testing.T) {
	t.Run("sends ErrorResponse and ReadyForQuery", func(t *testing.T) {
		var buf bytes.Buffer
		sendError(&buf, "CREATE DATABASE statements are unsupported")

		// Parse the written bytes: should be ErrorResponse then ReadyForQuery.
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&buf),
			io.Discard,
		)

		msg1, err := frontend.Receive()
		require.NoError(t, err)
		errResp, ok := msg1.(*pgproto3.ErrorResponse)
		require.True(t, ok, "expected ErrorResponse, got %T", msg1)
		assert.Equal(t, "ERROR", errResp.Severity)
		assert.Equal(t, "0A000", errResp.Code)
		assert.Equal(t, "CREATE DATABASE statements are unsupported", errResp.Message)

		msg2, err := frontend.Receive()
		require.NoError(t, err)
		rfq, ok := msg2.(*pgproto3.ReadyForQuery)
		require.True(t, ok, "expected ReadyForQuery, got %T", msg2)
		assert.Equal(t, byte('I'), rfq.TxStatus)
	})

	t.Run("does not panic on write failure", func(t *testing.T) {
		// sendError should gracefully handle write failures.
		sendError(&failWriter{}, "some error")
	})
}

func TestDrainUntilSync(t *testing.T) {
	t.Run("drains Bind, Execute, then Sync", func(t *testing.T) {
		var buf bytes.Buffer
		// Write Bind, Describe, Execute, Sync.
		bind := &pgproto3.Bind{}
		b, _ := bind.Encode(nil)
		buf.Write(b)

		desc := &pgproto3.Describe{ObjectType: 'P'}
		b, _ = desc.Encode(nil)
		buf.Write(b)

		exec := &pgproto3.Execute{}
		b, _ = exec.Encode(nil)
		buf.Write(b)

		sync := &pgproto3.Sync{}
		b, _ = sync.Encode(nil)
		buf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&buf),
			io.Discard,
		)
		drainUntilSync(backend)
		// If it returns without hanging, the test passes.
	})

	t.Run("returns on read error", func(t *testing.T) {
		var empty bytes.Buffer
		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&empty),
			io.Discard,
		)
		drainUntilSync(backend)
		// Should return without hanging.
	})
}

func TestClientToBackend(t *testing.T) {
	t.Run("forwards allowed Query to backend", func(t *testing.T) {
		// Build a client stream with an allowed Query followed by Terminate.
		var clientBuf bytes.Buffer
		q := &pgproto3.Query{String: "SELECT 1"}
		b, _ := q.Encode(nil)
		clientBuf.Write(b)

		term := &pgproto3.Terminate{}
		b, _ = term.Encode(nil)
		clientBuf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&clientBuf),
			io.Discard,
		)

		var backendBuf bytes.Buffer
		var clientRespBuf bytes.Buffer

		ctx := context.Background()
		clientToBackend(ctx, backend, &backendBuf, &clientRespBuf, newLastSQL())

		// The allowed Query + Terminate should have been forwarded to backendBuf.
		assert.True(t, backendBuf.Len() > 0, "expected data forwarded to backend")
		// No error response to client for allowed queries.
		assert.Equal(t, 0, clientRespBuf.Len(), "expected no error sent to client")
	})

	t.Run("blocks disallowed Query and sends error to client", func(t *testing.T) {
		var clientBuf bytes.Buffer
		q := &pgproto3.Query{String: "CREATE EXTENSION pgcrypto"}
		b, _ := q.Encode(nil)
		clientBuf.Write(b)

		term := &pgproto3.Terminate{}
		b, _ = term.Encode(nil)
		clientBuf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&clientBuf),
			io.Discard,
		)

		var backendBuf bytes.Buffer
		var clientRespBuf bytes.Buffer

		ctx := context.Background()
		clientToBackend(ctx, backend, &backendBuf, &clientRespBuf, newLastSQL())

		// Blocked query should NOT be forwarded. Only Terminate is forwarded.
		// Parse what was sent to the client: should be ErrorResponse + ReadyForQuery.
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&clientRespBuf),
			io.Discard,
		)
		msg, err := frontend.Receive()
		require.NoError(t, err)
		errResp, ok := msg.(*pgproto3.ErrorResponse)
		require.True(t, ok)
		assert.Equal(t, "CREATE EXTENSION statements are unsupported", errResp.Message)
		assert.Equal(t, "0A000", errResp.Code)
	})

	t.Run("blocks disallowed Parse and drains until Sync", func(t *testing.T) {
		var clientBuf bytes.Buffer
		p := &pgproto3.Parse{Query: "CREATE DATABASE mydb"}
		b, _ := p.Encode(nil)
		clientBuf.Write(b)

		bind := &pgproto3.Bind{}
		b, _ = bind.Encode(nil)
		clientBuf.Write(b)

		exec := &pgproto3.Execute{}
		b, _ = exec.Encode(nil)
		clientBuf.Write(b)

		sync := &pgproto3.Sync{}
		b, _ = sync.Encode(nil)
		clientBuf.Write(b)

		term := &pgproto3.Terminate{}
		b, _ = term.Encode(nil)
		clientBuf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&clientBuf),
			io.Discard,
		)

		var backendBuf bytes.Buffer
		var clientRespBuf bytes.Buffer

		ctx := context.Background()
		clientToBackend(ctx, backend, &backendBuf, &clientRespBuf, newLastSQL())

		// Error sent to client.
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&clientRespBuf),
			io.Discard,
		)
		msg, err := frontend.Receive()
		require.NoError(t, err)
		errResp, ok := msg.(*pgproto3.ErrorResponse)
		require.True(t, ok)
		assert.Equal(t, "CREATE DATABASE statements are unsupported", errResp.Message)
	})

	t.Run("context cancellation stops relay", func(t *testing.T) {
		// Use net.Pipe so Receive() blocks until cancelled.
		clientSide, proxySide := net.Pipe()
		defer clientSide.Close()
		defer proxySide.Close()

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(proxySide),
			proxySide,
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			clientToBackend(ctx, backend, io.Discard, io.Discard, newLastSQL())
			close(done)
		}()

		// Cancel should cause the function to return.
		cancel()
		// Close the pipe to unblock any pending read.
		clientSide.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("clientToBackend did not return after context cancellation")
		}
	})

	t.Run("returns on backend write failure", func(t *testing.T) {
		var clientBuf bytes.Buffer
		q := &pgproto3.Query{String: "SELECT 1"}
		b, _ := q.Encode(nil)
		clientBuf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&clientBuf),
			io.Discard,
		)

		ctx := context.Background()
		clientToBackend(ctx, backend, &failWriter{}, io.Discard, newLastSQL())
		// Should return without hanging.
	})

	t.Run("forwards allowed Parse to backend", func(t *testing.T) {
		var clientBuf bytes.Buffer
		p := &pgproto3.Parse{Name: "stmt1", Query: "SELECT 1"}
		b, _ := p.Encode(nil)
		clientBuf.Write(b)

		term := &pgproto3.Terminate{}
		b, _ = term.Encode(nil)
		clientBuf.Write(b)

		backend := pgproto3.NewBackend(
			pgproto3.NewChunkReader(&clientBuf),
			io.Discard,
		)

		var backendBuf bytes.Buffer
		var clientRespBuf bytes.Buffer

		ctx := context.Background()
		clientToBackend(ctx, backend, &backendBuf, &clientRespBuf, newLastSQL())

		assert.True(t, backendBuf.Len() > 0)
		assert.Equal(t, 0, clientRespBuf.Len())
	})
}

func TestBackendToClient(t *testing.T) {
	t.Run("relays messages from backend to client", func(t *testing.T) {
		var backendBuf bytes.Buffer
		cc := &pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}
		b, _ := cc.Encode(nil)
		backendBuf.Write(b)

		rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
		b, _ = rfq.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&backendBuf),
			io.Discard,
		)

		var clientBuf bytes.Buffer
		ctx := context.Background()
		backendToClient(ctx, frontend, &clientBuf, newLastSQL())

		// Should have written data to client.
		assert.True(t, clientBuf.Len() > 0)
	})

	t.Run("returns on client write failure", func(t *testing.T) {
		var backendBuf bytes.Buffer
		cc := &pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}
		b, _ := cc.Encode(nil)
		backendBuf.Write(b)

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(&backendBuf),
			io.Discard,
		)

		ctx := context.Background()
		backendToClient(ctx, frontend, &failWriter{}, newLastSQL())
		// Should return without hanging.
	})

	t.Run("context cancellation stops relay", func(t *testing.T) {
		serverSide, proxySide := net.Pipe()
		defer serverSide.Close()
		defer proxySide.Close()

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(proxySide),
			proxySide,
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			backendToClient(ctx, frontend, io.Discard, newLastSQL())
			close(done)
		}()

		cancel()
		serverSide.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("backendToClient did not return after context cancellation")
		}
	})
}

func TestHandleConnection(t *testing.T) {
	t.Run("full proxy flow with allowed query", func(t *testing.T) {
		ln := mockBackend(t, pgBackendHandler(t))
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), nil)
			close(done)
		}()

		// Send startup message.
		clientConn.Write(buildStartupMessage("postgres", "postgres"))

		// Read AuthenticationOk.
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(clientConn),
			clientConn,
		)
		msg, err := frontend.Receive()
		require.NoError(t, err)
		_, ok := msg.(*pgproto3.AuthenticationOk)
		require.True(t, ok, "expected AuthenticationOk, got %T", msg)

		// Read ReadyForQuery.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok, "expected ReadyForQuery, got %T", msg)

		// Send an allowed query.
		q := &pgproto3.Query{String: "SELECT 1"}
		buf, _ := q.Encode(nil)
		clientConn.Write(buf)

		// Read CommandComplete.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		cc, ok := msg.(*pgproto3.CommandComplete)
		require.True(t, ok, "expected CommandComplete, got %T", msg)
		assert.Equal(t, "SELECT 1", string(cc.CommandTag))

		// Read ReadyForQuery.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok)

		// Send terminate.
		term := &pgproto3.Terminate{}
		buf, _ = term.Encode(nil)
		clientConn.Write(buf)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return")
		}
	})

	t.Run("full proxy flow with blocked query", func(t *testing.T) {
		ln := mockBackend(t, pgBackendHandler(t))
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), nil)
			close(done)
		}()

		// Startup.
		clientConn.Write(buildStartupMessage("postgres", "postgres"))
		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(clientConn),
			clientConn,
		)
		// Consume AuthenticationOk + ReadyForQuery.
		frontend.Receive()
		frontend.Receive()

		// Send a blocked query.
		q := &pgproto3.Query{String: "CREATE EXTENSION pgcrypto"}
		buf, _ := q.Encode(nil)
		clientConn.Write(buf)

		// Should receive ErrorResponse.
		msg, err := frontend.Receive()
		require.NoError(t, err)
		errResp, ok := msg.(*pgproto3.ErrorResponse)
		require.True(t, ok, "expected ErrorResponse, got %T", msg)
		assert.Equal(t, "CREATE EXTENSION statements are unsupported", errResp.Message)
		assert.Equal(t, "0A000", errResp.Code)

		// Should receive ReadyForQuery.
		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok)

		// Send terminate.
		term := &pgproto3.Terminate{}
		buf, _ = term.Encode(nil)
		clientConn.Write(buf)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return")
		}
	})

	t.Run("SSL request negotiation", func(t *testing.T) {
		ln := mockBackend(t, pgBackendHandler(t))
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), nil)
			close(done)
		}()

		// Send SSLRequest first.
		clientConn.Write(buildSSLRequest())

		// Read the 'N' response.
		resp := make([]byte, 1)
		_, err := io.ReadFull(clientConn, resp)
		require.NoError(t, err)
		assert.Equal(t, byte('N'), resp[0])

		// Now send the real startup.
		clientConn.Write(buildStartupMessage("postgres", "postgres"))

		frontend := pgproto3.NewFrontend(
			pgproto3.NewChunkReader(clientConn),
			clientConn,
		)
		// Should get AuthenticationOk then ReadyForQuery.
		msg, err := frontend.Receive()
		require.NoError(t, err)
		_, ok := msg.(*pgproto3.AuthenticationOk)
		require.True(t, ok, "expected AuthenticationOk after SSL negotiation, got %T", msg)

		msg, err = frontend.Receive()
		require.NoError(t, err)
		_, ok = msg.(*pgproto3.ReadyForQuery)
		require.True(t, ok)

		// Terminate.
		term := &pgproto3.Terminate{}
		buf, _ := term.Encode(nil)
		clientConn.Write(buf)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return")
		}
	})

	t.Run("startup read error", func(t *testing.T) {
		// Close client side immediately so proxy gets read error.
		clientConn, proxyConn := net.Pipe()
		clientConn.Close()

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, "127.0.0.1:1", nil)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("handleConnection did not return on startup read error")
		}
	})

	t.Run("upstream connection failure", func(t *testing.T) {
		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			// Point to an address that will refuse connections.
			handleConnection(context.Background(), proxyConn, "127.0.0.1:1", nil)
			close(done)
		}()

		// Send startup message.
		clientConn.Write(buildStartupMessage("postgres", "postgres"))

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handleConnection did not return on upstream failure")
		}
	})

	t.Run("cancel request forwarded to backend", func(t *testing.T) {
		received := make(chan []byte, 1)
		ln := mockBackend(t, func(conn net.Conn) {
			defer conn.Close()
			buf := make([]byte, 16)
			io.ReadFull(conn, buf)
			received <- buf
		})
		defer ln.Close()

		clientConn, proxyConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			handleConnection(context.Background(), proxyConn, ln.Addr().String(), nil)
			close(done)
		}()

		cancelMsg := buildCancelRequest(42, 99)
		clientConn.Write(cancelMsg)

		select {
		case got := <-received:
			assert.Equal(t, cancelMsg, got)
		case <-time.After(2 * time.Second):
			t.Fatal("backend did not receive cancel request")
		}

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("handleConnection did not return after cancel request")
		}
	})
}
