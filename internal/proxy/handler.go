package proxy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/jackc/pgproto3/v2"

	"tomodian/dsql-migrate/internal/ui"
)

const (
	sslRequestCode    = 80877103
	cancelRequestCode = 80877102
)

func handleConnection(ctx context.Context, clientConn net.Conn, upstreamAddr string) {
	defer clientConn.Close()

	// Read the initial startup message raw bytes.
	startupBytes, code, err := readStartupMessage(clientConn)
	if err != nil {
		ui.Dim("    Failed to read startup: %v\n", err)
		return
	}

	// Handle SSLRequest: respond with 'N' (no SSL) and read the real startup.
	if code == sslRequestCode {
		if _, err := clientConn.Write([]byte{'N'}); err != nil {
			return
		}
		startupBytes, code, err = readStartupMessage(clientConn)
		if err != nil {
			ui.Dim("    Failed to read startup after SSL: %v\n", err)
			return
		}
	}

	// Handle CancelRequest: forward to backend and close.
	if code == cancelRequestCode {
		if backendConn, err := net.Dial("tcp", upstreamAddr); err == nil {
			backendConn.Write(startupBytes)
			backendConn.Close()
		}
		return
	}

	// Connect to the upstream PostgreSQL.
	backendConn, err := net.Dial("tcp", upstreamAddr)
	if err != nil {
		ui.Error("Failed to connect to upstream %s: %v", upstreamAddr, err)
		return
	}
	defer backendConn.Close()

	// Forward the startup message to the backend.
	if _, err := backendConn.Write(startupBytes); err != nil {
		return
	}

	// Create protocol handlers for post-startup message flow.
	frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(backendConn), backendConn)
	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(clientConn), clientConn)

	// Relay authentication messages until ReadyForQuery.
	if err := relayAuth(frontend, clientConn); err != nil {
		ui.Dim("    Auth relay failed: %v\n", err)
		return
	}

	ui.Dim("    Client connected via proxy\n")

	// Steady-state relay with two goroutines.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			cancel()
			clientConn.Close()
			backendConn.Close()
		})
	}

	// Backend -> Client relay.
	go func() {
		defer closeAll()
		backendToClient(ctx, frontend, clientConn)
	}()

	// Client -> Backend relay (runs on this goroutine).
	defer closeAll()
	clientToBackend(ctx, backend, backendConn, clientConn)
}

// readStartupMessage reads a raw PostgreSQL startup message (no type byte,
// just length + payload). Returns the full raw bytes and the protocol/request code.
func readStartupMessage(r io.Reader) ([]byte, uint32, error) {
	// Read 4-byte length.
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, 0, err
	}
	msgLen := binary.BigEndian.Uint32(lenBuf[:])

	// Read the rest of the message.
	buf := make([]byte, msgLen)
	copy(buf[:4], lenBuf[:])
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, 0, err
	}

	// The protocol version / request code is at bytes 4-7.
	code := binary.BigEndian.Uint32(buf[4:8])
	return buf, code, nil
}

// relayAuth forwards backend messages to the client until ReadyForQuery.
func relayAuth(frontend *pgproto3.Frontend, clientConn io.Writer) error {
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return err
		}
		buf, err := msg.Encode(nil)
		if err != nil {
			return err
		}
		if _, err := clientConn.Write(buf); err != nil {
			return err
		}
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			return nil
		}
	}
}

// clientToBackend reads messages from the client, inspects Query/Parse, and
// either blocks or forwards them to the backend.
func clientToBackend(ctx context.Context, backend *pgproto3.Backend, backendConn io.Writer, clientConn io.Writer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := backend.Receive()
		if err != nil {
			return
		}

		switch m := msg.(type) {
		case *pgproto3.Query:
			if errMsg := Check(m.String); errMsg != "" {
				ui.Dim("    Blocked: %s\n", errMsg)
				sendError(clientConn, errMsg)
				continue
			}

		case *pgproto3.Parse:
			if errMsg := Check(m.Query); errMsg != "" {
				ui.Dim("    Blocked: %s\n", errMsg)
				sendError(clientConn, errMsg)
				// Drain messages until Sync to keep protocol aligned.
				drainUntilSync(backend)
				continue
			}

		case *pgproto3.Terminate:
			// Forward and exit.
			if buf, err := m.Encode(nil); err == nil {
				backendConn.Write(buf)
			}
			return
		}

		// Forward the message to the backend.
		buf, err := msg.Encode(nil)
		if err != nil {
			return
		}
		if _, err := backendConn.Write(buf); err != nil {
			return
		}
	}
}

// backendToClient reads messages from the backend and forwards them to the client.
func backendToClient(ctx context.Context, frontend *pgproto3.Frontend, clientConn io.Writer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := frontend.Receive()
		if err != nil {
			return
		}
		buf, err := msg.Encode(nil)
		if err != nil {
			return
		}
		if _, err := clientConn.Write(buf); err != nil {
			return
		}
	}
}

// sendError writes an ErrorResponse (SQLSTATE 0A000) followed by ReadyForQuery
// to the client connection.
func sendError(w io.Writer, message string) {
	errResp := &pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     "0A000",
		Message:  message,
	}
	buf, err := errResp.Encode(nil)
	if err != nil {
		return
	}

	rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
	buf, err = rfq.Encode(buf)
	if err != nil {
		return
	}

	w.Write(buf)
}

// drainUntilSync reads and discards client messages until a Sync is received.
// This is needed after rejecting a Parse message in the extended query protocol.
func drainUntilSync(backend *pgproto3.Backend) {
	for {
		msg, err := backend.Receive()
		if err != nil {
			return
		}
		if _, ok := msg.(*pgproto3.Sync); ok {
			return
		}
	}
}
