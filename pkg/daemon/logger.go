package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawbridge/pkg/vault"
)

const (
	DefaultMaxLogBytes = 10 * 1024 * 1024 // 10MB
	DefaultMaxBackups  = 3
)

// RotatingLogger is a thread-safe, size-capped rotating file writer.
type RotatingLogger struct {
	mu         sync.Mutex
	filePath   string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

// NewRotatingLogger creates or opens a rotating log file.
func NewRotatingLogger(filePath string, maxBytes int64, maxBackups int) (*RotatingLogger, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxLogBytes
	}
	if maxBackups <= 0 {
		maxBackups = DefaultMaxBackups
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &RotatingLogger{
		filePath:   filePath,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
		file:       f,
		size:       fi.Size(),
	}, nil
}

// Write appends bytes to the file and triggers rotation if size exceeds maxBytes.
func (l *RotatingLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	writeLen := int64(len(p))
	if l.size+writeLen > l.maxBytes {
		if err := l.rotateLocked(); err != nil {
			return 0, fmt.Errorf("log rotation failed: %w", err)
		}
	}

	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *RotatingLogger) rotateLocked() error {
	_ = l.file.Close()

	// Shift older backups: .2 -> .3, .1 -> .2, etc.
	for i := l.maxBackups; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", l.filePath, i)
		dst := fmt.Sprintf("%s.%d", l.filePath, i+1)
		if i == l.maxBackups {
			_ = os.Remove(src)
		} else {
			if _, err := os.Stat(src); err == nil {
				_ = os.Rename(src, dst)
			}
		}
	}

	// Rename current file to .1
	backup1 := fmt.Sprintf("%s.1", l.filePath)
	_ = os.Rename(l.filePath, backup1)

	// Open fresh file
	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	l.file = f
	l.size = 0
	return nil
}

// Close closes the underlying log file.
func (l *RotatingLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// CheckpointWAL truncates the SQLite WAL log to keep disk space minimal.
func CheckpointWAL(dbMgr *vault.DatabaseManager) error {
	if dbMgr == nil || dbMgr.DB() == nil {
		return nil
	}
	_, err := dbMgr.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// StartWALCheckpointer periodically checkpoints SQLite WAL in the background.
func StartWALCheckpointer(ctx context.Context, dbMgr *vault.DatabaseManager, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = CheckpointWAL(dbMgr)
			}
		}
	}()
}

