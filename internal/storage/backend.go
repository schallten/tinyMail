package storage

// Package storage provides the mail storage abstraction layer.
// It defines the Backend interface that protocol engines use to
// persist and retrieve messages, decoupling them from the
// underlying filesystem implementation.

import (
	"context"
	"time"
)

