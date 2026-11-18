// Package product holds the menu: what can be ordered, and at what price.
package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrNotFound means no product has the requested ID.
var ErrNotFound = errors.New("product not found")

// Product is an item on the menu.
//
// Price is kept in minor units (1250 means 12.50). Whole numbers are exact,
// so totals and discounts never pick up floating-point errors. The API
// converts to a decimal number only when it writes JSON.
type Product struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	PriceMinor int64  `json:"priceMinor"`
}

// Repository reads the menu. It is an interface so a database can replace
// the in-memory version without touching the HTTP layer.
type Repository interface {
	List(ctx context.Context) ([]Product, error)
	Get(ctx context.Context, id int64) (Product, error)
}

// MemoryRepository holds the menu in memory. It is never changed after it is
// built, so it is safe for concurrent use.
type MemoryRepository struct {
	list []Product // in menu order
	byID map[int64]Product
}

// NewMemoryRepository checks the products and returns a repository holding
// them in the order given.
func NewMemoryRepository(products []Product) (*MemoryRepository, error) {
	r := &MemoryRepository{byID: make(map[int64]Product, len(products))}
	for _, p := range products {
		switch {
		case p.ID < 1:
			return nil, fmt.Errorf("product %q: ID must be positive, got %d", p.Name, p.ID)
		case p.Name == "":
			return nil, fmt.Errorf("product %d: name is empty", p.ID)
		case p.PriceMinor < 0:
			return nil, fmt.Errorf("product %d: price is negative", p.ID)
		}
		if _, dup := r.byID[p.ID]; dup {
			return nil, fmt.Errorf("product ID %d appears twice", p.ID)
		}
		r.byID[p.ID] = p
		r.list = append(r.list, p)
	}
	return r, nil
}

// LoadFile reads a menu from a JSON file: an array of products with "id",
// "name", "category" and "priceMinor". Unknown fields are rejected, so a typo
// in the file fails loudly instead of loading a product with a missing price.
func LoadFile(path string) (*MemoryRepository, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("loading products: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var products []Product
	if err := dec.Decode(&products); err != nil {
		return nil, fmt.Errorf("loading products from %s: %w", path, err)
	}
	return NewMemoryRepository(products)
}

// List returns every product in menu order.
func (r *MemoryRepository) List(context.Context) ([]Product, error) {
	return append([]Product(nil), r.list...), nil // a copy, so callers can't change the menu
}

// Get returns the product with the given ID, or ErrNotFound.
func (r *MemoryRepository) Get(_ context.Context, id int64) (Product, error) {
	p, ok := r.byID[id]
	if !ok {
		return Product{}, ErrNotFound
	}
	return p, nil
}
