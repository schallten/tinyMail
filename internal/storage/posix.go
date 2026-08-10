package storage

import (
	"context"
	"os"
	"path/filepath"
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

func (s *POSIXStorage) WriteMessage(ctx context.Context, user string, msg *Message) (string, error) {
	// TODO: Atomic write implementation
	return "", nil
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