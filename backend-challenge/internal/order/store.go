package order

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
)

// MemoryRepository keeps orders in memory, guarded by a lock so many
// requests can save at once. Orders are lost when the server restarts; a
// database would replace this for production.
type MemoryRepository struct {
	mu     sync.RWMutex
	orders map[string]Order
}

// NewMemoryRepository returns an empty repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{orders: make(map[string]Order)}
}

// Save stores o. Saving the same ID twice is an error, so a duplicate ID can
// never silently overwrite an order.
func (r *MemoryRepository) Save(_ context.Context, o Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.orders[o.ID]; exists {
		return fmt.Errorf("order %s already exists", o.ID)
	}
	r.orders[o.ID] = o
	return nil
}

// Get returns a saved order. The API has no endpoint for this yet; it is
// here for tests and for a future GET /order/{id}.
func (r *MemoryRepository) Get(_ context.Context, id string) (Order, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.orders[id]
	return o, ok
}

// Len returns how many orders are saved.
func (r *MemoryRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.orders)
}

// NewUUID returns a random (version 4) UUID. It is unguessable and needs no
// shared counter, so any number of server instances can create IDs.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("reading random bytes: %v", err)) // the OS random source is broken
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
