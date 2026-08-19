// Package pop3 implements an RFC 1939 compliant POP3 server engine.
// Each TCP connection is handled by a dedicated POP3Session with its own
// phase state machine, buffered I/O, and mailbox snapshot.
package pop3

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tinymail/internal/storage"
)

// Phase constants for the POP3 session.
// Unlike SMTP's fine-grained states, POP3 has only 3 coarse phases.

const (
	PhaseAuthorize   = iota // awaiting user then pass
	PhaseTransaction        // authetnicated , can start work
	PhaseUpdate             // automatic on quit , commits deleteion
)

// POP3Session holds per connection state for one POP3 client.
// created fresh for each accepted TCP connection , never reused
type POP3Session struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	// currrent phase ( auth - trans - upd)
	phase int

	//auth state
	user     string
	password string // stored temporarily

	// mailbox snapshot loaded at login , frozen for session dureation
	// indices are 1 based per RFC 1939 , slice is 0 based by default
	mailbox []storage.MessageMetadata

	// deletion tracking : maps message id -> marked for deletion
	// only commited to dfisk on successfull QUIT
	toDelete map[string]bool

	// storage backend
	store storage.Backend

	// metadata
	remoteAddr string
	startTime  time.Time
}

// NewPOP3Session creates a session from an accepted TCP connection
// sets intiial phase to authorize and sends the OK greeint immediately
func NewPOP3Session(conn net.Conn, store storage.Backend) *POP3Session {
	s := &POP3Session{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		writer:     bufio.NewWriter(conn),
		phase:      PhaseAuthorize,
		toDelete:   make(map[string]bool),
		store:      store,
		remoteAddr: conn.RemoteAddr().String(),
		startTime:  time.Now(),
	}
	// greeting before client input due to RFC 1939
	s.writeResponse(true, "POP3 server ready")
	return s
}

// run until quit or timeout
// single entry point called from connection handler goroutine
func (s *POP3Session) Run() {
	defer s.conn.Close()

	for {
		// 30s timeout
		s.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		line, err := s.reader.ReadString('\n')
		if err != nil {
			// timeout
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if err := s.processCommand(line); err != nil {
			break
			// sever error
		}
		// QUIT handler sets phase to PhaseUpdate and returns nil
		// break here after update completes
		if s.phase == PhaseUpdate {
			break
		}
	}
}

// processCommand parses a raw line and dispatches to the appropriate handler.
// Command verbs are case-insensitive; arguments are case-preserved.
func (s *POP3Session) processCommand(line string) error {
	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToUpper(parts[0])

	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "USER":
		return s.handleUser(args)
	case "PASS":
		return s.handlePass(args)
	case "STAT":
		return s.handleStat()
	case "LIST":
		return s.handleList(args)
	case "RETR":
		return s.handleRetr(args)
	case "DELE":
		return s.handleDele(args)
	case "NOOP":
		return s.writeResponse(true, "OK")
	case "RSET":
		return s.handleReset()
	case "QUIT":
		return s.handleQuit()
	default:
		return s.writeResponse(false, "Unknown command")
	}
}

// writeResponse sends a formatted POP3 response and flushes the buffer.
// Format: "+OK {MESSAGE}\r\n" or "-ERR {MESSAGE}\r\n" per RFC 1939.
// Flush is mandatory; buffered writer won't send without it.
func (s *POP3Session) writeResponse(ok bool, message string) error {
	prefix := "+OK"
	if !ok {
		prefix = "-ERR"
	}
	resp := fmt.Sprintf("%s %s\r\n", prefix, message)
	if _, err := s.writer.WriteString(resp); err != nil {
		return err
	}
	return s.writer.Flush()
}

// handleUser processes the USER command in AUTHORIZE phase.
// Stores username temporarily; actual validation happens on PASS.
// RFC 1939 §7: USER must precede PASS; re-issuing USER resets state.
func (s *POP3Session) handleUser(username string) error {
	if s.phase != PhaseAuthorize {
		return s.writeResponse(false, "command valid only in auth state")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return s.writeResponse(false, "syntax: USER <username>")
	}
	s.user = username
	return s.writeResponse(true, "user accepted")
}

// handlePass processes the PASS command in AUTHORIZE phase.
// Validates credentials, loads mailbox snapshot, transitions to TRANSACTION.
// Failed auth resets user field; client must re-send USER before retrying PASS.

func (s *POP3Session) handlePass(password string) error {
	if s.phase != PhaseAuthorize || s.user == "" {
		return s.writeResponse(false, "Command valid only after USER")
	}
	password = strings.TrimSpace(password)

	// Validate against plaintext credential file
	if !validateCredentials(s.user, password) {
		s.user = "" // Reset; force re-USER on next attempt
		return s.writeResponse(false, "Invalid username or password")
	}

	// Load mailbox snapshot; frozen for entire session duration.
	// Indices remain stable even if another client deletes messages concurrently.
	messages, err := s.store.ListMessages(s.user)
	if err != nil {
		s.user = ""
		return s.writeResponse(false, "Service unavailable")
	}

	s.mailbox = messages
	s.toDelete = make(map[string]bool) // Fresh deletion map for this session
	s.phase = PhaseTransaction
	return s.writeResponse(true, fmt.Sprintf("Logged in, %d messages", len(messages)))
}

// validateCredentials checks username:password against storage/.config/users.db.
// Returns false if file missing, user not found, or password mismatch.
func validateCredentials(user, pass string) bool {
	data, err := os.ReadFile(filepath.Join("storage", ".config", "users.db"))
	if err != nil {
		return false
	}
	target := user + ":" + pass
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func (s *POP3Session) handleStat() error {
	return s.writeResponse(false, "Not implemented yet")
}

func (s *POP3Session) handleList(args string) error {
	return s.writeResponse(false, "Not implemented yet")
}

func (s *POP3Session) handleRetr(args string) error {
	return s.writeResponse(false, "Not implemented yet")
}

func (s *POP3Session) handleDele(args string) error {
	return s.writeResponse(false, "Not implemented yet")
}

func (s *POP3Session) handleReset() error {
	return s.writeResponse(false, "Not implemented yet")
}

func (s *POP3Session) handleQuit() error {
	return s.writeResponse(false, "Not implemented yet")
}
