package main

import (
	"sync"
)

type ChairCache struct {
	mu    sync.RWMutex
	cache map[string]Chair
}

var chairCache = ChairCache{
	cache: make(map[string]Chair),
}

func (c *ChairCache) Get(id string) (Chair, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	chair, found := c.cache[id]
	return chair, found
}

func (c *ChairCache) Store(id string, chair Chair) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[id] = chair
}

func (c *ChairCache) Delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, id)
}

func (c *ChairCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]Chair)
}
