package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// POSIXStorage implements Backend using the local filesystem.
// Each user gets isolated tmp/ and inbox/ subdirectories.
// All writes are atomic: stage in tmp/, fsync, then rename to inbox/.
type POSIXStorage struct {
	BaseDir string // Root storage path, e.g., "./storage"
}

// creat the base directory if it doesnt exit
func NewPOSIXStorage(baseDir string) *POSIXStorage {
	// ensire base dir exists on construction
	os.MkdirAll(baseDir, 0755)
	return &POSIXStorage{BaseDir: baseDir}
}

// userDIrs ensures tmp/ and inbox/ exist for a given user
// called before any read write ops
func (s *POSIXStorage) userDirs(user string) (tmpDir, inboxDir string, err error) {
	tmpDir = filepath.Join(s.BaseDir, user, "tmp")
	inboxDir = filepath.Join(s.BaseDir, user, "inbox")
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(inboxDir, 0750); err != nil {
		return "", "", err
	}
	return tmpDir, inboxDir, nil
}

// skeleton to be iimplementated later

// WriteMessage atomically persists a message to a user's inbox.
// Flow: serialize → write to tmp/ → fsync → rename to inbox/.
// The rename is atomic on POSIX; if the server crashes before rename,
// only tmp/ contains a partial file and inbox/ remains clean.
func (s *POSIXStorage) WriteMessage(ctx context.Context, user string, msg *Message) (string, error) {
	// ensure user dirs exist
	tmpDir, inboxDir, err := s.userDirs(user)
	if err != nil {
		return "", fmt.Errorf("create user dirs: %w", err)
	}
	// generate immutable , sortable filename
	// format mention in readme
	// hash to prevent collisions for same second submissions
	hash := hashMessageContent(msg)
	filename := fmt.Sprintf("%d_%s.eml", time.Now().Unix(), hash)

	// serialize messages into RFC 2822 format
	body := serializeMessage(msg)

	// write to temporary staging file
	tmpPath := filepath.Join(tmpDir, filename+".tmp")
	f, err := os.Create(tmpPath)

	if err != nil {
		return "", fmt.Errorf("create tmp file : %w", err)
	}

	// 5. Stream body to disk (not buffered in memory beyond OS page cache)
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		os.Remove(tmpPath) // clean up on failure
		return "", fmt.Errorf("fsync: %w", err)
	}
	f.Close() // must close before rename
	// 7. Atomic rename: tmp → inbox
	//    On POSIX, this is a single metadata operation.
	//    Either the old name exists or the new name exists; never both/neither.
	finalPath := filepath.Join(inboxDir, filename)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("atomic rename: %w", err)
	}

	return filename, nil
}

// serializeMessage converts a Message struct into RFC 2822 formatted bytes.
// Headers and body are separated by a blank line (\r\n\r\n).
func serializeMessage(msg *Message) string {
	var b strings.Builder
	b.WriteString("From: " + msg.From + "\r\n")
	b.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if msg.Subject != "" {
		b.WriteString("Subject: " + msg.Subject + "\r\n")
	}
	b.WriteString("Date: " + time.Now().Format(time.RFC2822) + "\r\n")
	b.WriteString("\r\n") // Blank line separates headers from body
	b.WriteString(msg.Body)
	return b.String()
}

// hashMessageContent generates an 8-char hex hash for collision avoidance.
// Uses From + Subject as content fingerprint (body excluded for performance).
func hashMessageContent(msg *Message) string {
	h := sha256.New()
	h.Write([]byte(msg.From + "|" + msg.Subject))
	sum := fmt.Sprintf("%x", h.Sum(nil))
	return sum[:8]
}

// ListMessages returns metadata for all messages in a user's inbox.
// Reads directory entries only (no file opens), making it O(n) fast.
// Results sorted chronologically by filename (timestamp prefix).
func (s *POSIXStorage) ListMessages(user string) ([]MessageMetadata, error) {
	inboxDir := filepath.Join(s.BaseDir, user, "inbox")
	// ReadDir returns entries sorted by name already on most FS
	// but we sort to guarantree
	entries, err := os.ReadDir(inboxDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MessageMetadata{}, nil // empty mailbox is alr
		}
		return nil, fmt.Errorf("read inbox dir: %w", err)
	}
	messages := make([]MessageMetadata, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip subdirs
		}
		info, err := entry.Info()
		if err != nil {
			continue // skip unreadable files
		}
		messages = append(messages, MessageMetadata{
			ID:        entry.Name(),
			SizeBytes: info.Size(),
			Timestamp: info.ModTime(),
		})
	}
	// sort by filename ( timestamp prefix ensure its )
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID < messages[j].ID
	})
	return messages, nil
}

// GetMessage retrieves raw .eml bytes by message ID.
// CRITICAL: Sanitizes ID to prevent directory traversal attacks.
// A malicious client could send "../../etc/passwd" as an ID.
func (s *POSIXStorage) GetMessage(user string, id string) ([]byte, error) {
	if !isValidMessageID(id) {
		return nil, fmt.Errorf("invalid message ID: contains path separators or traversal")

	}
	path:= filepath.Join(s.BaseDir,user,"inbox",id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read message file: %w", err)
	}
	return data, nil
}

// isValidMessageID checks that an ID cannot escape the inbox directory.
// Rejects any ID containing "/" , "\" , or ".." path components.
func isValidMessageID(id string) bool {
	if id == "" {
		return false
	}
	if strings.ContainsAny(id, "/\\") {
		return false
	}
	if strings.Contains(id, "..") {
		return false
	}
	return true
}


// DeleteMessage removes a message file from disk.
// Called during POP3 UPDATE phase after QUIT command.
// Same sanitization as GetMessage to prevent arbitrary file deletion.
func (s *POSIXStorage) DeleteMessage(user string, id string) error {
	if !isValidMessageID(id) {
		return fmt.Errorf("invalid message ID: contains path separators or traversal")
	}
	path := filepath.Join(s.BaseDir, user, "inbox", id)
	if err:=os.Remove(path); err!=nil && !os.IsNotExist(err){
		return fmt.Errorf("delete message : %w",err)
	}
	return nil
}

func (s *POSIXStorage) UserExists(user string) bool {
	info, err := os.Stat(filepath.Join(s.BaseDir, user))
	return err == nil && info.IsDir()
}
