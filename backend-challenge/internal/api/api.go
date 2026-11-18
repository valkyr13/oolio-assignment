// Package api is the HTTP layer. It turns requests into calls on the domain
// packages, and their results into the JSON the OpenAPI spec defines. No
// business rule lives here; it only translates.
package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/valkyr13/oolio-assignment/internal/auth"
	"github.com/valkyr13/oolio-assignment/internal/order"
	"github.com/valkyr13/oolio-assignment/internal/product"
)

// DefaultMaxBodyBytes is the largest order request accepted. The largest
// valid order (50 lines of about 40 bytes, plus a coupon) is about 3 KB, so
// 16 KB is generous and still tiny.
const DefaultMaxBodyBytes = 16 << 10

// OrderPlacer places orders. order.Service implements it.
type OrderPlacer interface {
	Place(ctx context.Context, req order.Request) (order.Order, error)
}

// Config lists what the HTTP layer needs. Every dependency is an interface,
// so tests and future storage can swap any of them.
type Config struct {
	Products     product.Repository
	Orders       OrderPlacer
	Keys         auth.KeyStore
	MaxBodyBytes int64        // defaults to DefaultMaxBodyBytes
	Logger       *slog.Logger // optional; nothing is logged if nil
}

type server struct {
	products     product.Repository
	orders       OrderPlacer
	keys         auth.KeyStore
	maxBodyBytes int64
	logger       *slog.Logger
}

// NewHandler returns the API's HTTP handler. Routes live under /api, the
// base path of the spec's server URL.
func NewHandler(cfg Config) http.Handler {
	s := &server{
		products:     cfg.Products,
		orders:       cfg.Orders,
		keys:         cfg.Keys,
		maxBodyBytes: cfg.MaxBodyBytes,
		logger:       cfg.Logger,
	}
	if s.maxBodyBytes <= 0 {
		s.maxBodyBytes = DefaultMaxBodyBytes
	}
	if s.logger == nil {
		s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/product", s.listProducts)
	mux.HandleFunc("GET /api/product/{productId}", s.getProduct)
	mux.HandleFunc("POST /api/order", s.requireScope(auth.ScopeCreateOrder, s.placeOrder))

	// The same paths with any other method get a JSON 405.
	mux.HandleFunc("/api/product", methodNotAllowed("GET"))
	mux.HandleFunc("/api/product/{productId}", methodNotAllowed("GET"))
	mux.HandleFunc("/api/order", methodNotAllowed("POST"))

	// Not part of the spec: lets Docker or Kubernetes check the server is up.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", s.notFound) // anything else: a JSON 404, like every other error

	return s.logRequests(s.recoverPanics(mux))
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, errNotFound, "No such endpoint")
}

// internalError logs the real cause and sends the client a generic message,
// so internal details never leak in a response.
func (s *server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, errInternal, "Internal server error")
}
