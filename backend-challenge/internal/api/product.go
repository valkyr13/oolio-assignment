package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/valkyr13/oolio-assignment/internal/product"
)

// productJSON is the spec's Product schema. It is kept separate from
// product.Product on purpose: the API sends the ID as a string and the price
// as a decimal number, while the domain keeps an int64 ID and the price in
// minor units. Either side can change without silently changing the other.
type productJSON struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Category string  `json:"category"`
}

func toProductJSON(p product.Product) productJSON {
	return productJSON{
		ID:       strconv.FormatInt(p.ID, 10),
		Name:     p.Name,
		Price:    float64(p.PriceMinor) / 100,
		Category: p.Category,
	}
}

// menuCacheControl lets browsers and CDNs reuse the menu for a minute. The
// menu is public and changes rarely, so this takes most read load off the
// server. Orders never read from a cache: they look prices up directly.
const menuCacheControl = "public, max-age=60"

func (s *server) listProducts(w http.ResponseWriter, r *http.Request) {
	products, err := s.products.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]productJSON, 0, len(products)) // an empty menu is [], not null
	for _, p := range products {
		out = append(out, toProductJSON(p))
	}
	w.Header().Set("Cache-Control", menuCacheControl)
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("productId"))
	if !ok {
		writeError(w, http.StatusBadRequest, errInvalidID, "Invalid ID supplied: it must be a positive whole number")
		return
	}
	p, err := s.products.Get(r.Context(), id)
	if errors.Is(err, product.ErrNotFound) {
		writeError(w, http.StatusNotFound, errNotFound, "Product not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", menuCacheControl)
	writeJSON(w, http.StatusOK, toProductJSON(p))
}
