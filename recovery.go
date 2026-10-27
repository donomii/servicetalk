package main

import (
	"fmt"
	"time"
)

func (c *Coordinator) recoveredWorkspace() (string, error) {
	roots := c.rootSnapshot()
	c.mu.RLock()
	defer c.mu.RUnlock()
	id := ""
	for _, root := range roots {
		if entry := c.registry[root]; entry != nil && entry.ClassName == BuiltinClassWorkspace {
			if id != "" {
				return "", fmt.Errorf("cannot select a recovered workspace: both %s and %s are pinned workspace roots", id, root)
			} else {
				id = root
			}
		} else {
			// Other roots do not own the main REPL workspace.
		}
	}
	return id, nil
}

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
