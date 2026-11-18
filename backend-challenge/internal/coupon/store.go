package coupon

import (
	"bufio"
	"context"
	"fmt"
	"os"
)

// Store answers whether a code is in the published list of valid coupons.
//
// The context and the error are there for stores that live elsewhere, such as
// a database or a cache service. The in-memory store never fails, but callers
// are written so that a networked store can replace it without any changes.
type Store interface {
	Contains(ctx context.Context, code string) (bool, error)
}

// MemoryStore holds the valid codes in a map, so a lookup takes nanoseconds.
// It is never changed after it is built, so it is safe for concurrent use.
type MemoryStore struct {
	codes map[string]struct{}
}

// NewMemoryStore returns a store holding codes, compared exactly as written.
func NewMemoryStore(codes []string) *MemoryStore {
	s := &MemoryStore{codes: make(map[string]struct{}, len(codes))}
	for _, c := range codes {
		s.codes[c] = struct{}{}
	}
	return s
}

// LoadFile reads a list published by cmd/ingest: one code per line, exactly
// as written. Blank lines are ignored.
func LoadFile(path string) (*MemoryStore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("loading coupons: %w", err)
	}
	defer f.Close()

	s := &MemoryStore{codes: make(map[string]struct{})}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if code := scanner.Text(); code != "" {
			s.codes[code] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("loading coupons from %s: %w", path, err)
	}
	return s, nil
}

// Contains reports whether code is a valid coupon. It never returns an error.
func (s *MemoryStore) Contains(_ context.Context, code string) (bool, error) {
	_, ok := s.codes[code]
	return ok, nil
}

// Len returns how many codes the store holds.
func (s *MemoryStore) Len() int { return len(s.codes) }
