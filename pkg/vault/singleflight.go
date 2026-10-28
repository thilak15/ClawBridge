package vault

import "sync"

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

// SingleFlightGroup deduplicates concurrent calls for the same key.
type SingleFlightGroup struct {
	mu    sync.Mutex
	calls map[string]*call
}

// NewSingleFlightGroup creates a new SingleFlightGroup.
func NewSingleFlightGroup() *SingleFlightGroup {
	return &SingleFlightGroup{
		calls: make(map[string]*call),
	}
}

// Execute runs the given function only once for the specified key among concurrent callers.
func (g *SingleFlightGroup) Execute(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if c, exists := g.calls[key]; exists {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}

	c := &call{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()

	return c.val, c.err
}
