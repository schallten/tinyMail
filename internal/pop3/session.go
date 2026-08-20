// Package pop3 implements an RFC 1939 compliant POP3 server engine.
// Each TCP connection is handled by a dedicated POP3Session with its own
// phase state machine, buffered I/O, and mailbox snapshot.
package pop3

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"tinymail/internal/log"
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
	logger     *log.Logger
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
		logger:     log.New("POP3"),
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

	// Strip domain for validation and storage lookup
	lookupUser := s.user
	if idx := strings.Index(lookupUser, "@"); idx > 0 {
		lookupUser = lookupUser[:idx]
	}

	if !validateCredentials(s.user, password) {
		s.user = ""
		return s.writeResponse(false, "Invalid username or password")
	}

	// Use stripped username for storage operations
	messages, err := s.store.ListMessages(lookupUser)
	if err != nil {
		s.user = ""
		return s.writeResponse(false, "Service unavailable")
	}

	s.user = lookupUser
	s.mailbox = messages
	s.toDelete = make(map[string]bool)
	s.phase = PhaseTransaction

	s.logger.Info(s.remoteAddr, "Login success",
		"user", s.user,
		"messages", fmt.Sprintf("%d", len(s.mailbox)),
	)
	return s.writeResponse(true, fmt.Sprintf("Logged in, %d messages", len(messages)))
}

// validateCredentials checks username:password against storage/.config/users.db.
// Returns false if file missing, user not found, or password mismatch.
func validateCredentials(user, pass string) bool {
	if idx := strings.Index(user, "@"); idx > 0 {
		user = user[:idx]
	}

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

// handleStat processes the STAT command in TRANSACTION phase.
// Returns count and total bytes of non-deleted messages only.
// Format: "+OK <count> <bytes>\r\n" per RFC 1939 §5.
func (s *POP3Session) handleStat() error {
	if s.phase != PhaseTransaction {
		return s.writeResponse(false, "Command valid only in TRANSACTION state")
	}

	count := 0
	var totalBytes int64
	for _, msg := range s.mailbox {
		if !s.toDelete[msg.ID] {
			count++
			totalBytes += msg.SizeBytes
		}
	}

	return s.writeResponse(true, fmt.Sprintf("%d %d", count, totalBytes))
}

// handleList processes the LIST command in TRANSACTION phase.
// Without argument: multi-line listing of all non-deleted messages.
// With argument: single-line response for specific message index.
// Indices are 1-based per RFC 1939; internally mapped to 0-based slice.
func (s *POP3Session) handleList(args string) error {
	if s.phase != PhaseTransaction {
		return s.writeResponse(false, "Command valid only in TRANSACTION state")
	}

	args = strings.TrimSpace(args)

	if args == "" {
		// Multi-line listing: "+OK\r\n<id> <size>\r\n...\r\n.\r\n"
		s.writer.WriteString("+OK\r\n")
		for i, msg := range s.mailbox {
			if !s.toDelete[msg.ID] {
				fmt.Fprintf(s.writer, "%d %d\r\n", i+1, msg.SizeBytes)
			}
		}
		s.writer.WriteString(".\r\n")
		return s.writer.Flush()
	}

	// Single-message lookup
	idx, err := strconv.Atoi(args)
	if err != nil || idx < 1 || idx > len(s.mailbox) {
		return s.writeResponse(false, "Invalid message number")
	}

	msg := s.mailbox[idx-1] // Convert 1-based → 0-based
	if s.toDelete[msg.ID] {
		return s.writeResponse(false, "Message not found")
	}

	return s.writeResponse(true, fmt.Sprintf("%d %d", idx, msg.SizeBytes))
}

// handleRetr processes the RETR command in TRANSACTION phase.
// Streams raw .eml bytes with RFC 1939 dot-stuffing applied on output.
// Any line in the body starting with "." gets an extra "." prepended
// so clients don't mistake it for the "\r\n.\r\n" terminator.
func (s *POP3Session) handleRetr(args string) error {
	if s.phase != PhaseTransaction {
		return s.writeResponse(false, "command valid only in transaction state")
	}
	idx, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || idx < 1 || idx > len(s.mailbox) {
		return s.writeResponse(false, "invalid message number")
	}

	msg := s.mailbox[idx-1] // convert 1 based to 0 based
	if s.toDelete[msg.ID] {
		return s.writeResponse(false, "message not found")
	}
	body, err := s.store.GetMessage(s.user, msg.ID)
	if err != nil {
		return s.writeResponse(false, "service unavaiable")
	}
	// send header with byte count
	fmt.Fprintf(s.writer, "+OK %d octets\r\n", len(body))

	// stream body line by line with dot stuffing on output
	// split on \n to preseve orgiainl line endings for re-assebly
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		// re-add \n except for last element ( which maybe empty)
		if i < len(lines)-1 {
			line = append(line, '\n')
		} else if len(line) == 0 {
			continue
		}

		// dot suffing, prepend "." if line starst with "."
		if len(line) > 0 && line[0] == '.' {
			s.writer.WriteByte('.')
		}
		s.writer.Write(line)
	}
	// Terminator: CRLF + "." + CRLF
	s.writer.WriteString("\r\n.\r\n")
	return s.writer.Flush()

}

// handleDele processes the DELE command in TRANSACTION phase.
// Marks message for deletion in memory ONLY; actual file removal
// happens in UPDATE phase on QUIT. This enables rollback on disconnect.
func (s *POP3Session) handleDele(args string) error {
	if s.phase != PhaseTransaction {
		return s.writeResponse(false, "command valid opnly in transaction state")
	}
	idx, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || idx < 1 || idx > len(s.mailbox) {
		return s.writeResponse(false, "invalid message number")
	}

	msg := s.mailbox[idx-1]
	if s.toDelete[msg.ID] {
		return s.writeResponse(false, "mssage already marked for deletion")
	}

	s.toDelete[msg.ID] = true
	return s.writeResponse(true, fmt.Sprintf("Message %d marked for deletion", idx))

}

// handleReset processes the RSET command in TRANSACTION phase.
// Clears all deletion marks without disconnecting.
// Messages marked via DELE become visible again in STAT/LIST/RETR.
func (s *POP3Session) handleReset() error {
	if s.phase != PhaseTransaction {
		return s.writeResponse(false, "Command valid only in TRANSACTION state")
	}
	s.toDelete = make(map[string]bool)
	return s.writeResponse(true, "Mailbox reset")
}

// handleQuit processes the QUIT command in any phase.
// In AUTHORIZE: simply closes connection.
// In TRANSACTION: enters UPDATE phase, commits all deferred deletions,
// then closes connection. Deletion failures are logged but don't fail QUIT.
func (s *POP3Session) handleQuit() error {
	if s.phase == PhaseAuthorize {
		s.writeResponse(true, "Bye")
		s.phase = PhaseUpdate
		return nil
	}

	// UPDATE phase: commit all deferred deletions to storage.
	// Best-effort: log failures but continue. A failed delete means
	// the message stays in inbox; client can re-delete next session.
	for id := range s.toDelete {
		if err := s.store.DeleteMessage(s.user, id); err != nil {
			// Log but don't abort; partial success is acceptable
			fmt.Fprintf(os.Stderr, "[POP3] Failed to delete %s for user %s: %v\n",
				id, s.user, err)
		}
	}
	s.writeResponse(true, "Bye")
	s.phase = PhaseUpdate
	return nil
}
