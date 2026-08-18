// Package smtp implements an RFC 5321 compliant SMTP server engine.
// Each TCP connection is handled by a dedicated SMTPSession with its own
// finite state machine, buffered I/O, and message context.

package smtp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

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
}

// NewSMTPSession crweates a session from an accepted TCP Connection
// sets Initial state to INIT and send the 220 greeint imediately
func NewSMTPSession(conn net.Conn) *SMTPSession {
	s := &SMTPSession{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		writer:     bufio.NewWriter(conn),
		state:      StateInit,
		remoteAddr: conn.RemoteAddr().String(),
		startTime:  time.Now(),
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
	return s.writeResponse(502, "Not implemented yet")
}

func (s *SMTPSession) handleReset() error {
	return s.writeResponse(502, "Not implemented yet")
}

func (s *SMTPSession) handleQuit() error {
	return s.writeResponse(502, "Not implemented yet")
}
