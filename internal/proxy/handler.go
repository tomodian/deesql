package proxy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgproto3/v2"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/secure/precis"

	"tomodian/deesql/internal/ui"
)

const (
	sslRequestCode    = 80877103
	cancelRequestCode = 80877102
)

// BypassConfig holds credentials for auth bypass mode.
// When set, the proxy accepts any client auth and connects to the backend
// using these credentials instead of relaying auth from the client.
type BypassConfig struct {
	User     string
	Password string
	Database string
}

type handleConnectionInput struct {
	ClientConn   net.Conn `validate:"required"`
	UpstreamAddr string   `validate:"required"`
	Bypass       *BypassConfig
}

func handleConnection(ctx context.Context, in handleConnectionInput) {
	clientConn := in.ClientConn
	upstreamAddr := in.UpstreamAddr
	bypass := in.Bypass
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

	// In bypass mode, build a new startup message with the bypass credentials
	// instead of forwarding the client's original startup.
	backendStartup := startupBytes
	if bypass != nil {
		backendStartup = buildStartupBytes(bypass.User, bypass.Database)
	}

	// Connect to the upstream PostgreSQL.
	backendConn, err := net.Dial("tcp", upstreamAddr)
	if err != nil {
		ui.Error("Failed to connect to upstream %s: %v", upstreamAddr, err)
		return
	}
	defer backendConn.Close()

	// Forward the startup message to the backend.
	if _, err := backendConn.Write(backendStartup); err != nil {
		return
	}

	// Create frontend for reading backend messages.
	frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(backendConn), backendConn)

	if bypass != nil {
		// Bypass mode: accept client unconditionally, handle backend auth with bypass creds.
		if err := bypassAuth(ctx, bypassAuthInput{
			Frontend:    frontend,
			ClientConn:  clientConn,
			BackendConn: backendConn,
			Password:    bypass.Password,
		}); err != nil {
			ui.Dim("    Bypass auth failed: %v\n", err)
			return
		}
	} else {
		// Normal mode: relay auth messages between client and backend.
		if err := relayAuth(frontend, clientConn); err != nil {
			ui.Dim("    Auth relay failed: %v\n", err)
			return
		}
	}

	// Create backend for reading client messages (after auth is complete).
	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(clientConn), clientConn)

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

	// Shared state for error context: the last SQL forwarded to the backend.
	var lastSQL atomic.Value
	lastSQL.Store("")

	// Backend -> Client relay.
	go func() {
		defer closeAll()
		backendToClient(ctx, backendToClientInput{
			Frontend:   frontend,
			ClientConn: clientConn,
			LastSQL:    &lastSQL,
		})
	}()

	// Client -> Backend relay (runs on this goroutine).
	defer closeAll()
	clientToBackend(ctx, clientToBackendInput{
		Backend:     backend,
		BackendConn: backendConn,
		ClientConn:  clientConn,
		LastSQL:     &lastSQL,
	})
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

// bypassAuth handles authentication in bypass mode:
//   - Sends AuthenticationOk to the client unconditionally (no password check).
//   - Authenticates with the backend using the bypass password (cleartext, MD5, or SCRAM-SHA-256).
//   - Forwards ParameterStatus, BackendKeyData, and ReadyForQuery to the client.
type bypassAuthInput struct {
	Frontend   *pgproto3.Frontend `validate:"required"`
	ClientConn io.Writer          `validate:"required"`
	BackendConn io.Writer         `validate:"required"`
	Password   string
}

func bypassAuth(ctx context.Context, in bypassAuthInput) error {
	frontend := in.Frontend
	clientConn := in.ClientConn
	backendConn := in.BackendConn
	password := in.Password
	// Send AuthenticationOk to the client immediately.
	authOk := &pgproto3.AuthenticationOk{}
	buf, err := authOk.Encode(nil)
	if err != nil {
		return err
	}
	if _, err := clientConn.Write(buf); err != nil {
		return err
	}

	// Handle backend auth and forward non-auth messages to client.
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return err
		}

		switch m := msg.(type) {
		case *pgproto3.AuthenticationCleartextPassword, *pgproto3.AuthenticationMD5Password:
			pw := &pgproto3.PasswordMessage{Password: password}
			buf, err := pw.Encode(nil)
			if err != nil {
				return err
			}
			if _, err := backendConn.Write(buf); err != nil {
				return err
			}

		case *pgproto3.AuthenticationSASL:
			if err := scramAuth(ctx, scramAuthInput{
				Frontend:    frontend,
				BackendConn: backendConn,
				Mechanisms:  m.AuthMechanisms,
				Password:    password,
			}); err != nil {
				return fmt.Errorf("SCRAM auth failed: %w", err)
			}

		case *pgproto3.AuthenticationOk:
			continue

		case *pgproto3.ErrorResponse:
			buf, err := msg.Encode(nil)
			if err != nil {
				return err
			}
			clientConn.Write(buf)
			return fmt.Errorf("backend auth failed: %s", m.Message)

		case *pgproto3.ReadyForQuery:
			buf, err := msg.Encode(nil)
			if err != nil {
				return err
			}
			if _, err := clientConn.Write(buf); err != nil {
				return err
			}
			return nil

		default:
			// ParameterStatus, BackendKeyData, etc. — forward to client.
			buf, err := msg.Encode(nil)
			if err != nil {
				return err
			}
			if _, err := clientConn.Write(buf); err != nil {
				return err
			}
		}
	}
}

// scramAuth performs SCRAM-SHA-256 authentication with the backend.
type scramAuthInput struct {
	Frontend    *pgproto3.Frontend `validate:"required"`
	BackendConn io.Writer          `validate:"required"`
	Mechanisms  []string           `validate:"required"`
	Password    string
}

func scramAuth(ctx context.Context, in scramAuthInput) error {
	frontend := in.Frontend
	backendConn := in.BackendConn
	mechanisms := in.Mechanisms
	password := in.Password
	hasSHA256 := false
	for _, m := range mechanisms {
		if m == "SCRAM-SHA-256" {
			hasSHA256 = true
			break
		}
	}
	if !hasSHA256 {
		return fmt.Errorf("server does not support SCRAM-SHA-256")
	}

	// Prepare password.
	pw, err := precis.OpaqueString.Bytes([]byte(password))
	if err != nil {
		pw = []byte(password)
	}

	// Generate client nonce.
	nonceBuf := make([]byte, 18)
	if _, err := rand.Read(nonceBuf); err != nil {
		return err
	}
	clientNonce := base64.RawStdEncoding.EncodeToString(nonceBuf)
	clientFirstBare := fmt.Sprintf("n=,r=%s", clientNonce)

	// Send SASLInitialResponse (client-first-message).
	initResp := &pgproto3.SASLInitialResponse{
		AuthMechanism: "SCRAM-SHA-256",
		Data:          []byte("n,," + clientFirstBare),
	}
	buf, err := initResp.Encode(nil)
	if err != nil {
		return err
	}
	if _, err := backendConn.Write(buf); err != nil {
		return err
	}

	// Receive AuthenticationSASLContinue (server-first-message).
	msg, err := frontend.Receive()
	if err != nil {
		return err
	}
	saslContinue, ok := msg.(*pgproto3.AuthenticationSASLContinue)
	if !ok {
		return fmt.Errorf("expected AuthenticationSASLContinue, got %T", msg)
	}

	// Parse server-first-message: r=<nonce>,s=<salt>,i=<iterations>
	serverFirst := saslContinue.Data
	parts := bytes.SplitN(serverFirst, []byte(","), 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid server-first-message")
	}
	if !bytes.HasPrefix(parts[0], []byte("r=")) || !bytes.HasPrefix(parts[1], []byte("s=")) || !bytes.HasPrefix(parts[2], []byte("i=")) {
		return fmt.Errorf("invalid server-first-message format")
	}
	combinedNonce := parts[0][2:]
	salt, err := base64.StdEncoding.DecodeString(string(parts[1][2:]))
	if err != nil {
		return fmt.Errorf("invalid salt: %w", err)
	}
	iterations, err := strconv.Atoi(string(parts[2][2:]))
	if err != nil {
		return fmt.Errorf("invalid iterations: %w", err)
	}

	// Compute client-final-message.
	clientFinalWithoutProof := fmt.Sprintf("c=biws,r=%s", combinedNonce)
	saltedPassword := pbkdf2.Key(pw, salt, iterations, 32, sha256.New)

	clientKey := computeHMAC(saltedPassword, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)

	authMessage := []byte(fmt.Sprintf("%s,%s,%s", clientFirstBare, serverFirst, clientFinalWithoutProof))
	clientSig := computeHMAC(storedKey[:], authMessage)

	proof := make([]byte, len(clientSig))
	for i := range clientSig {
		proof[i] = clientKey[i] ^ clientSig[i]
	}

	clientFinal := fmt.Sprintf("%s,p=%s", clientFinalWithoutProof, base64.StdEncoding.EncodeToString(proof))

	// Send SASLResponse (client-final-message).
	saslResp := &pgproto3.SASLResponse{Data: []byte(clientFinal)}
	buf, err = saslResp.Encode(nil)
	if err != nil {
		return err
	}
	if _, err := backendConn.Write(buf); err != nil {
		return err
	}

	// Receive AuthenticationSASLFinal (server-final-message) — verify server signature.
	msg, err = frontend.Receive()
	if err != nil {
		return err
	}
	saslFinal, ok := msg.(*pgproto3.AuthenticationSASLFinal)
	if !ok {
		return fmt.Errorf("expected AuthenticationSASLFinal, got %T", msg)
	}

	serverKey := computeHMAC(saltedPassword, []byte("Server Key"))
	expectedSig := computeHMAC(serverKey, authMessage)
	expectedSigB64 := base64.StdEncoding.EncodeToString(expectedSig)
	if !bytes.HasPrefix(saslFinal.Data, []byte("v=")) {
		return fmt.Errorf("invalid server-final-message")
	}
	if string(saslFinal.Data[2:]) != expectedSigB64 {
		return fmt.Errorf("server signature mismatch")
	}

	return nil
}

func computeHMAC(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

// buildStartupBytes constructs a raw PostgreSQL StartupMessage for the given user and database.
func buildStartupBytes(user, database string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 0})              // placeholder for length
	binary.Write(&buf, binary.BigEndian, uint32(196608)) // protocol 3.0
	buf.WriteString("user")
	buf.WriteByte(0)
	buf.WriteString(user)
	buf.WriteByte(0)
	if database != "" {
		buf.WriteString("database")
		buf.WriteByte(0)
		buf.WriteString(database)
		buf.WriteByte(0)
	}
	buf.WriteByte(0) // terminal null
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	return b
}

// clientToBackend reads messages from the client, inspects Query/Parse, and
// either blocks or forwards them to the backend.
type clientToBackendInput struct {
	Backend    *pgproto3.Backend `validate:"required"`
	BackendConn io.Writer        `validate:"required"`
	ClientConn io.Writer         `validate:"required"`
	LastSQL    *atomic.Value     `validate:"required"`
}

func clientToBackend(ctx context.Context, in clientToBackendInput) {
	backend := in.Backend
	backendConn := in.BackendConn
	clientConn := in.ClientConn
	lastSQL := in.LastSQL
	var tx TxState

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
			ui.Dim("    --> %s\n", truncateSQL(m.String))
			lastSQL.Store(m.String)
			if errMsg := Check(m.String); errMsg != "" {
				ui.Warn("[deesql] %s", errMsg)
				sendError(clientConn, errMsg)
				continue
			}
			// Warn about transaction rule violations (DDL/DML mixing).
			for _, stmt := range splitStatements(m.String) {
				if warn := tx.TrackTx(stmt); warn != "" {
					ui.Warn("[deesql] %s", warn)
				}
			}
			m.String = Rewrite(m.String)
			msg = m

		case *pgproto3.Parse:
			if m.Query != "" {
				ui.Dim("    --> %s\n", truncateSQL(m.Query))
				lastSQL.Store(m.Query)
			}
			if errMsg := Check(m.Query); errMsg != "" {
				ui.Warn("[deesql] %s", errMsg)
				sendError(clientConn, errMsg)
				// Drain messages until Sync to keep protocol aligned.
				drainUntilSync(backend)
				continue
			}
			if warn := tx.TrackTx(m.Query); warn != "" {
				ui.Warn("[deesql] %s", warn)
			}
			m.Query = Rewrite(m.Query)
			msg = m

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
type backendToClientInput struct {
	Frontend   *pgproto3.Frontend `validate:"required"`
	ClientConn io.Writer          `validate:"required"`
	LastSQL    *atomic.Value      `validate:"required"`
}

func backendToClient(ctx context.Context, in backendToClientInput) {
	frontend := in.Frontend
	clientConn := in.ClientConn
	lastSQL := in.LastSQL
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

		// Log errors from PostgreSQL with context so users can diagnose issues.
		if e, ok := msg.(*pgproto3.ErrorResponse); ok {
			logPostgresError(e, lastSQL.Load().(string))
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

// logPostgresError logs a PostgreSQL ErrorResponse with context about why it may
// have failed, including DSQL-specific hints when the SQL uses DSQL syntax that
// PostgreSQL does not understand.
func logPostgresError(e *pgproto3.ErrorResponse, sql string) {
	ui.Error("[postgres] %s: %s", e.Code, e.Message)
	if e.Detail != "" {
		ui.Error("  Detail: %s", e.Detail)
	}
	if e.Hint != "" {
		ui.Error("  Hint: %s", e.Hint)
	}
	// Add DSQL-specific hints for known mismatches.
	if hint := dsqlHint(sql); hint != "" {
		ui.Warn("  %s", hint)
	}
}

// dsqlHintPatterns maps DSQL-specific SQL patterns to explanations of why
// they fail on PostgreSQL.
var dsqlHintPatterns = []struct {
	pattern *regexp.Regexp
	hint    string
}{}

func dsqlHint(sql string) string {
	for _, p := range dsqlHintPatterns {
		if p.pattern.MatchString(sql) {
			return p.hint
		}
	}
	return ""
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

// truncateSQL returns sql truncated to a reasonable log length.
func truncateSQL(sql string) string {
	// Collapse whitespace for cleaner logs.
	s := strings.TrimSpace(sql)
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
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
