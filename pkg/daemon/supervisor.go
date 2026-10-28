package daemon

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

const DefaultRestartDelay = 2 * time.Second

// Supervisor monitors worker goroutines and automatically recovers/respawns from panics.
type Supervisor struct {
	mu           sync.RWMutex
	restartDelay time.Duration
	logger       func(format string, args ...any)
	panicCounts  map[string]int
}

// NewSupervisor creates a new Supervisor instance.
func NewSupervisor(restartDelay time.Duration, logger func(format string, args ...any)) *Supervisor {
	if restartDelay <= 0 {
		restartDelay = DefaultRestartDelay
	}
	if logger == nil {
		logger = func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}
	return &Supervisor{
		restartDelay: restartDelay,
		logger:       logger,
		panicCounts:  make(map[string]int),
	}
}

// Supervise runs a named worker function in a loop, catching panics and restarting after restartDelay.
func (s *Supervisor) Supervise(ctx context.Context, name string, worker func(ctx context.Context) error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.runWorkerProtected(ctx, name, worker)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Supervisor) runWorkerProtected(ctx context.Context, name string, worker func(ctx context.Context) error) {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.panicCounts[name]++
			s.mu.Unlock()

			stack := debug.Stack()
			s.logger("[Supervisor] Caught panic in worker '%s': %v\nStack trace:\n%s", name, r, string(stack))

			select {
			case <-ctx.Done():
			case <-time.After(s.restartDelay):
			}
		}
	}()

	err := worker(ctx)
	if err != nil && ctx.Err() == nil {
		s.logger("[Supervisor] Worker '%s' stopped with error: %v", name, err)
		select {
		case <-ctx.Done():
		case <-time.After(s.restartDelay):
		}
	}
}

// PanicCount returns the number of panics recorded for a worker.
func (s *Supervisor) PanicCount(name string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.panicCounts[name]
}

