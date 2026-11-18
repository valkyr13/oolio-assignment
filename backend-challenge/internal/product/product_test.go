package product

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewMemoryRepositoryRejectsBadData(t *testing.T) {
	tests := []struct {
		name     string
		products []Product
	}{
		{"zero ID", []Product{{ID: 0, Name: "Tea", PriceMinor: 100}}},
		{"negative ID", []Product{{ID: -1, Name: "Tea", PriceMinor: 100}}},
		{"empty name", []Product{{ID: 1, PriceMinor: 100}}},
		{"negative price", []Product{{ID: 1, Name: "Tea", PriceMinor: -1}}},
		{"duplicate ID", []Product{{ID: 1, Name: "Tea"}, {ID: 1, Name: "Coffee"}}},
	}
	for _, tt := range tests {
		if _, err := NewMemoryRepository(tt.products); err == nil {
			t.Errorf("%s: expected an error", tt.name)
		}
	}
}

func TestGetAndList(t *testing.T) {
	repo, err := NewMemoryRepository([]Product{
		{ID: 2, Name: "Coffee", Category: "Drinks", PriceMinor: 250},
		{ID: 1, Name: "Tea", Category: "Drinks", PriceMinor: 150},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	p, err := repo.Get(ctx, 1)
	if err != nil || p.Name != "Tea" {
		t.Fatalf("Get(1) = %+v, %v", p, err)
	}
	if _, err := repo.Get(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(99) error = %v, want ErrNotFound", err)
	}

	list, _ := repo.List(ctx)
	if len(list) != 2 || list[0].ID != 2 {
		t.Fatalf("List() = %+v, want both products in the order given", list)
	}
	list[0].Name = "changed"
	again, _ := repo.List(ctx)
	if again[0].Name != "Coffee" {
		t.Fatal("changing the returned list changed the menu")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`[{"id": 1, "name": "Tea", "category": "Drinks", "priceMinor": 150}]`), 0o644)
	repo, err := LoadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.Get(context.Background(), 1); p.PriceMinor != 150 {
		t.Fatalf("price = %d, want 150", p.PriceMinor)
	}

	typo := filepath.Join(dir, "typo.json")
	os.WriteFile(typo, []byte(`[{"id": 1, "name": "Tea", "category": "Drinks", "price": 1.5}]`), 0o644)
	if _, err := LoadFile(typo); err == nil {
		t.Fatal("an unknown field should be rejected")
	}
}

// The seed menu shipped with the server must always load.
func TestSeedMenuLoads(t *testing.T) {
	repo, err := LoadFile(filepath.Join("..", "..", "data", "products.json"))
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.List(context.Background()); len(list) == 0 {
		t.Fatal("the seed menu is empty")
	}
}
