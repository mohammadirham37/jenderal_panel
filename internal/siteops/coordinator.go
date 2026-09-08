// Package siteops coordinates filesystem mutations that belong to a website.
package siteops

import "sync"

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// Coordinator serializes mutations for the same website while allowing
// unrelated websites to progress independently.
type Coordinator struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

// Default is shared by website and SSL services created by the application.
var Default = NewCoordinator()

func NewCoordinator() *Coordinator {
	return &Coordinator{locks: make(map[string]*keyedLock)}
}

// Lock acquires the lock for websiteID and returns an idempotent unlock func.
func (c *Coordinator) Lock(websiteID string) func() {
	c.mu.Lock()
	entry := c.locks[websiteID]
	if entry == nil {
		entry = &keyedLock{}
		c.locks[websiteID] = entry
	}
	entry.refs++
	c.mu.Unlock()

	entry.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu.Unlock()
			c.mu.Lock()
			entry.refs--
			if entry.refs == 0 {
				delete(c.locks, websiteID)
			}
			c.mu.Unlock()
		})
	}
}
