package storage

import (
	"context"
	"os"
	"path/filepath"
	"crypto/sha256"
	"fmt"
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
	tmpDir = filepath.Join(s.BaseDir,user,"tmp")
	inboxDir = filepath.Join(s.BaseDir,user,"inbox")
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
	tmpDir,inboxDir,err:=s.userDirs(user)
	if err!=nil{
		return "",fmt.Errorf("create user dirs: %w",err)
	}
	// generate immutable , sortable filename
	// format mention in readme
	// hash to prevent collisions for same second submissions
	hash:=hashMessageContent(msg)
	filename:=fmt.Sprintf("%d_%s.eml",time.Now().Unix(),hash)

	// serialize messages into RFC 2822 format
	body:=serializeMessage(msg)

	// write to temporary staging file
	tmpPath := filepath.Join(tmpDir,filename+".tmp")
	f,err := os.Create(tmpPath)

	if err!=nil {
		return "",fmt.Errorf("create tmp file : %w",err)
	}

	// 5. Stream body to disk (not buffered in memory beyond OS page cache)
	if _,err := f.WriteString(body); err!= nil{
		f.Close()
		os.Remove(tmpPath) // clean up on failure
		return "", fmt.Errorf("fsync: %w",err)
	}
	f.Close() // must close before rename
	// 7. Atomic rename: tmp → inbox
	//    On POSIX, this is a single metadata operation.
	//    Either the old name exists or the new name exists; never both/neither.
	finalPath := filepath.Join(inboxDir,filename)
	if err:=os.Rename(tmpPath,finalPath); err!=nil{
		os.Remove(tmpPath)
		return "", fmt.Errorf("atomic rename: %w",err)
	}

	return filename,nil
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

func (s *POSIXStorage) ListMessages(user string) ([]MessageMetadata, error) {
	// TODO: Directory listing with metadata extraction
	return nil, nil
}

func (s *POSIXStorage) GetMessage(user string, id string) ([]byte, error) {
	// TODO: Sanitized file read
	return nil, nil
}

func (s *POSIXStorage) DeleteMessage(user string, id string) error {
	// TODO: Sanitized file deletion
	return nil
}

func (s *POSIXStorage) UserExists(user string) bool {
	info, err := os.Stat(filepath.Join(s.BaseDir, user))
	return err == nil && info.IsDir()
}