// Package smtp implements an RFC 5321 compliant SMTP server engine.
// Each TCP connection is handled by a dedicated SMTPSession with its own
// finite state machine, buffered I/O, and message context.

package smtp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"tinymail/internal/storage"
)

// MaxLineLength enforces RFC 5321 §4.5.3.1.6.
// Lines exceeding 1000 bytes (including CRLF) must be rejected.
// Prevents memory exhaustion from malicious or broken clients.
const MaxLineLength = 1000

// FSM state constants for the SMTP session.
// States are integers for fast comparison in the command loop.
// FSM - Finite State Machine ( unlike http which is new on each req this maintains a state or soething like that)
const (
	StateInit      = iota // tcp connection , awaiting HELO,EHLO
	StateGreeted          // identity established , ready fore MAIL FROM
	StateMail             // sender accepted , awaiting RCPT to
	StateRcpt             // >=1 recipient accepted , awaitinfg DATA
	StateBuffering        // receiving body lines until loine "."
	StateClosed           // connection termination
)

// smtp sesion holds per connection state for one smtp client
// create fresh for each accepted tcp connection , never reused
type SMTPSession struct {
	// buffered i/o : for line based protocol
	// conn.read() splits mid line , bufio guiarantess complete lines
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	// FSM current state
	state int

	//  message contexct ( reset after each succesfful data)
	from string
	to   []string

	// metdata for loggin and timeout
	remoteAddr string
	startTime  time.Time
	storage    storage.Backend // depencendy for message persistence
}

// NewSMTPSession crweates a session from an accepted TCP Connection
// sets Initial state to INIT and send the 220 greeint imediately
func NewSMTPSession(conn net.Conn, store storage.Backend) *SMTPSession {
	s := &SMTPSession{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		writer:     bufio.NewWriter(conn),
		state:      StateInit,
		remoteAddr: conn.RemoteAddr().String(),
		startTime:  time.Now(),
		storage:    store,
	}
	// greeting is sent before any client inpuot ( RFC 5321 >4.2)
	s.writeResponse(220, "localhost ESMTP ready")
	return s
}

// run executes the main command loop until QUIT , error or timeout.
// this si the single entry point called from the connection handler goroutine
func (s *SMTPSession) Run() {
	defer s.conn.Close()

	for {
		// timeout prevents idle client ffrom holding routines forever
		// 30s is the standard for command phjase , DATA phase extends this
		s.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := s.reader.ReadString('\n')
		if err != nil {
			// timeoout , eof or something
			break
		}
		// RFC 5321 §4.5.3.1.6: reject lines > 1000 bytes
		// Check BEFORE trimming to catch oversized input early
		if len(line) > MaxLineLength {
			s.writeResponse(500, "Line too long")
			break // Close connection on protocol violation
		}
		// strip trailing CRLF before parsing
		line = strings.TrimRight(line, "\r\n")

		if err := s.processCommand(line); err != nil {
			// severe error , close connection ( write failure , etc)
			break
		}
		if s.state == StateClosed {
			break
		}

	}

}

// processCommand parses a raw line and dispatches to the appropriate handler.
// Command verbs are case-insensitive; arguments are case-preserved.
func (s *SMTPSession) processCommand(line string) error {
	// split on first space only:
	// "MAIL FROM:<x>" -> ["MAIL","FROM:<x>"]
	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToUpper(parts[0])

	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "EHLO", "HELO":
		return s.handleGreeting(args)
	case "MAIL":
		return s.handleMailFrom(args)
	case "RCPT":
		return s.handleRcptTo(args)
	case "DATA":
		return s.handleData()
	case "RSET":
		return s.handleReset()
	case "QUIT":
		return s.handleQuit()
	case "NOOP":
		return s.writeResponse(250, "OK")
	default:
		return s.writeResponse(500, "Syntax error, command unrecognized")
	}
}

// writeResponse sends a formatted SMTP response and flushes the buffer.
// Format: "{CODE} {MESSAGE}\r\n" per RFC 5321 §4.2.
// Flush is mandatory; buffered writer won't send without it.
func (s *SMTPSession) writeResponse(code int, message string) error {
	resp := fmt.Sprintf("%d %s\r\n", code, message)
	if _, err := s.writer.WriteString(resp); err != nil {
		return err
	}
	return s.writer.Flush()
}

// handleGreeting processes EHLO/HELO commands.
// Transitions INIT → GREETED. Must be first command after connection.
// Returns multi-line capability list for EHLO, single-line for HELO.
func (s *SMTPSession) handleGreeting(args string) error {
	if s.state != StateInit {
		return s.writeResponse(503, "bad sequence of commands")
	}
	if args == "" {
		return s.writeResponse(501, "syntax: EHLO hostname")
	}
	s.state = StateGreeted

	// Multi-line response uses "-" continuation; final line uses space.
	// Clients parse capabilities from these lines (e.g., PIPELINING).
	s.writer.WriteString("250-localhost\r\n")
	s.writer.WriteString("250-PIPELINING\r\n")
	s.writer.WriteString("250 OK\r\n")
	return s.writer.Flush()
}

// handleMailFrom processes "MAIL FROM:<address>" command.
// Transitions GREETED → MAIL. Resets previous message context.
// Address must be wrapped in angle brackets per RFC 5321 §4.1.1.2.
func (s *SMTPSession) handleMailFrom(args string) error {
	if s.state != StateGreeted {
		return s.writeResponse(503, "Bad sequence of commands")
	}
	from := extractAddress(args)
	if from == "" {
		return s.writeResponse(501, "syntax: MAIL FROM:<address>")
	}
	// reset state for new message transaction
	s.from = from
	s.to = nil // clear any leftover recipients
	s.state = StateMail
	return s.writeResponse(250, "Sender accepted")
}

// handleRcptTo processes "RCPT TO:<address>" command.
// Transitions MAIL → RCPT or RCPT → RCPT (multiple recipients).
// At least one successful RCPT TO is required before DATA.

func (s *SMTPSession) handleRcptTo(args string) error {
	if s.state != StateMail && s.state != StateRcpt {
		// /this could have been written  better
		return s.writeResponse(503, "bad sequence of commands")
	}
	to := extractAddress(args)
	if to == "" {
		return s.writeResponse(501, "syntax: RCPT TO:<address")
	}
	s.to = append(s.to, to)
	s.state = StateRcpt
	return s.writeResponse(250, "recipient accepted")

}

// extractAddress parses "<address>" from command arguments.
// Handles both "FROM:<alice@example.com>" and bare "<alice@example.com>".
// Returns empty string if angle brackets are missing or malformed.
func extractAddress(arg string) string {
	start := strings.Index(arg, "<")
	end := strings.Index(arg, ">")
	if start < 0 || end <= start {
		return ""
	}
	return arg[start+1 : end]
}

func (s *SMTPSession) handleData() error {
	if s.state != StateRcpt || len(s.to) == 0 {
		return s.writeResponse(503, "bad sequence of commands")
	}

	s.state = StateBuffering
	if err := s.writeResponse(354, "Start mail input; end with <CRLF>.<CRLF>"); err != nil {
		return err
	}

	// Extend timeout for DATA phase: large messages take time to transmit.
	// 60s per line-read prevents slow clients from being killed mid-transfer.
	s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	var bodyBuilder strings.Builder
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			// timeout or connection reset during data -> abort
			return s.writeResponse(421, "Serviice unavailable , closing connectiioin")
		}
		// strip trailing crlf for processinig
		trimmed := strings.TrimRight(line, "\r\n")

		// terminator : single "." on a line signals end of data
		if trimmed == "." {
			break
		}

		// dot stuffing ".." at line start becomes "."
		// this prevents body content from being mistaken for the terminator
		if strings.HasPrefix(trimmed, ".") && len(trimmed) > 1 && trimmed[1] == '.' {
			trimmed = trimmed[1:]
		}
		bodyBuilder.WriteString(trimmed)
		bodyBuilder.WriteString("\r\n")
	}
	// extract subject fdrom accumulated body for storage metadata
	subject := extractSubject(bodyBuilder.String())

	// determine target user from first recipient ( single user delivery )
	// multii recipient fan out is a future enhancement
	user := strings.Split(s.to[0], "@")[0]
	msg := &storage.Message{
		From:    s.from,
		To:      s.to,
		Subject: subject,
		Body:    bodyBuilder.String(),
	}
	// Write atomically to storage (tmp → fsync → rename)
	if _, err := s.storage.WriteMessage(context.Background(), user, msg); err != nil {
		// Storage failure → 451 (local error, try again later)
		s.writeResponse(451, "Requested action aborted: local error")
		s.state = StateGreeted // Allow client to retry or QUIT cleanly
		return nil
	}

	// Success → reset transaction state for next message on same connection
	s.writeResponse(250, "Message queued")
	s.state = StateGreeted
	s.from = ""
	s.to = nil
	return nil
}

// extractSubject parses the Subject header from raw message body.
// Returns empty string if no Subject header is present.
func extractSubject(body string) string {
	for _, line := range strings.Split(body, "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			return strings.TrimPrefix(line, "Subject: ")
		}
	}
	return ""
}

// handleReset processes the RSET command.
// Aborts current transaction and returns to GREETED state.
// Does NOT close the connection; client can start a new message.
func (s *SMTPSession) handleReset() error {
	s.from = ""
	s.to = nil
	s.state = StateGreeted
	return s.writeResponse(250, "OK")
}

// handleQuit processes the QUIT command.
// Sends 221 response and transitions to CLOSED state.
// The actual conn.Close() happens via defer in Run().
func (s *SMTPSession) handleQuit() error {
	s.writeResponse(221, "Bye")
	s.state = StateClosed
	return nil
}
