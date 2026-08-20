// Package log provides structured logging for the mail server.
// Format: [COMPONENT] [REMOTE_ADDR] MESSAGE | key=value key=value
// Designed for grep-friendly output and future swap to slog/zap.
package log

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Logger wraps standard log.Logger with structured field support.
type Logger struct {
	component string // e.g., "SMTP", "POP3", "STORAGE"
	logger    *log.Logger
}

// New creates a logger for a specific component.
// Output goes to stderr to avoid interfering with protocol stdout.
func New(component string) *Logger {
	return &Logger{
		component: component,
		logger:    log.New(os.Stderr, "", log.LstdFlags),
	}
}

// Info logs an informational message with optional key-value fields.
// Example: l.Info("Message queued", "from", "alice@example.com", "to", "bob@example.com")
// Output: 2026/08/20 14:30:00 [SMTP] [192.168.1.5:43210] Message queued | from=alice@example.com to=bob@example.com
func (l *Logger) Info(remoteAddr, msg string, fields ...string) {
	l.log("INFO", remoteAddr, msg, fields...)
}

// Warn logs a warning message with optional key-value fields.
func (l *Logger) Warn(remoteAddr, msg string, fields ...string) {
	l.log("WARN", remoteAddr, msg, fields...)
}

// Error logs an error message with optional key-value fields.
func (l *Logger) Error(remoteAddr, msg string, fields ...string) {
	l.log("ERROR", remoteAddr, msg, fields...)
}

// log formats and outputs a structured log line.
// Fields are passed as alternating key-value pairs: "key1", "val1", "key2", "val2"
func (l *Logger) log(level, remoteAddr, msg string, fields ...string) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] [%s] %s", level, l.component, remoteAddr))
	if msg != "" {
		b.WriteString(" ")
		b.WriteString(msg)
	}
	if len(fields) > 0 {
		b.WriteString(" |")
		for i := 0; i+1 < len(fields); i += 2 {
			b.WriteString(" ")
			b.WriteString(fields[i])
			b.WriteString("=")
			b.WriteString(fields[i+1])
		}
	}
	l.logger.Println(b.String())
}
