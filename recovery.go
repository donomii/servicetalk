package main

import "time"

const staleReconnectGrace = 5 * time.Second

func (c *Coordinator) clearStaleFailure(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.staleFailures, id)
}

func (c *Coordinator) confirmStaleFailure(id string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.staleFailures == nil {
		c.staleFailures = make(map[string]time.Time)
	} else {
		// Preserve suspicion times for other objects.
	}
	if since, seen := c.staleFailures[id]; seen {
		return now.Sub(since) >= staleReconnectGrace
	} else {
		c.staleFailures[id] = now
		return false
	}
}
