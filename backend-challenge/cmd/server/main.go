// Command server runs the food-ordering API.
//
// All configuration comes from environment variables, so the same build runs
// unchanged on a laptop, in Docker or in Kubernetes:
//
//	ADDR                  listen address (default ":8080")
//	API_KEYS              keys for POST /api/order, e.g. "apitest:create_order,readonly:"
//	PRODUCTS_FILE         the menu (default "data/products.json")
//	COUPONS_FILE          valid coupon codes from cmd/ingest (default "data/valid_coupons.txt")
//	ORDER_MAX_LINES       lines per order (default 50)
//	ORDER_MAX_QUANTITY    quantity per product (default 100)
//	ORDER_MAX_BODY_BYTES  largest request body (default 16384)
//	LOG_FORMAT            "json" (default) or "text"
//
// On SIGINT or SIGTERM it stops accepting connections and lets requests in
// progress finish, for up to 10 seconds.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/valkyr13/oolio-assignment/internal/api"
	"github.com/valkyr13/oolio-assignment/internal/auth"
	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/order"
	"github.com/valkyr13/oolio-assignment/internal/product"
)

type config struct {
	Addr             string
	APIKeys          string
	ProductsFile     string
	CouponsFile      string
	OrderMaxLines    int
	OrderMaxQuantity int
	MaxBodyBytes     int64
	LogFormat        string
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Addr:         or(getenv("ADDR"), ":8080"),
		APIKeys:      getenv("API_KEYS"),
		ProductsFile: or(getenv("PRODUCTS_FILE"), "data/products.json"),
		CouponsFile:  or(getenv("COUPONS_FILE"), "data/valid_coupons.txt"),
		LogFormat:    or(getenv("LOG_FORMAT"), "json"),
	}
	var err error
	if cfg.OrderMaxLines, err = intVar(getenv, "ORDER_MAX_LINES", order.DefaultLimits.MaxLines); err != nil {
		return config{}, err
	}
	if cfg.OrderMaxQuantity, err = intVar(getenv, "ORDER_MAX_QUANTITY", order.DefaultLimits.MaxQuantity); err != nil {
		return config{}, err
	}
	body, err := intVar(getenv, "ORDER_MAX_BODY_BYTES", api.DefaultMaxBodyBytes)
	if err != nil {
		return config{}, err
	}
	cfg.MaxBodyBytes = int64(body)
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return config{}, fmt.Errorf("LOG_FORMAT must be json or text, got %q", cfg.LogFormat)
	}
	return cfg, nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intVar(getenv func(string) string, name string, fallback int) (int, error) {
	s := getenv(name)
	if s == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive whole number, got %q", name, s)
	}
	return n, nil
}

func newLogger(format string) *slog.Logger {
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(2)
	}
	logger := newLogger(cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, logger, nil); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// run starts the server and blocks until ctx is cancelled. If ready is not
// nil, the listening address is sent on it once the server accepts
// connections (tests use this with ADDR=127.0.0.1:0).
func run(ctx context.Context, cfg config, logger *slog.Logger, ready chan<- string) error {
	// Load everything before listening, so a bad file stops the server at
	// startup instead of failing requests later.
	products, err := product.LoadFile(cfg.ProductsFile)
	if err != nil {
		return err
	}
	coupons, err := coupon.LoadFile(cfg.CouponsFile)
	if err != nil {
		return err
	}
	keys, err := auth.ParseKeys(cfg.APIKeys)
	if err != nil {
		return err
	}
	if keys.Len() == 0 {
		logger.Warn("no API keys configured (API_KEYS): every POST /api/order will get 401")
	}

	orders := &order.Service{
		Products: products,
		Coupons:  coupon.DefaultValidator(coupon.DefaultRule, coupons),
		Orders:   order.NewMemoryRepository(),
		Limits:   order.Limits{MaxLines: cfg.OrderMaxLines, MaxQuantity: cfg.OrderMaxQuantity},
	}
	handler := api.NewHandler(api.Config{
		Products:     products,
		Orders:       orders,
		Keys:         keys,
		MaxBodyBytes: cfg.MaxBodyBytes,
		Logger:       logger,
	})

	srv := &http.Server{
		Handler: handler,
		// Timeouts stop slow or stalled clients from holding connections
		// open forever (for example a "slowloris" attack).
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	logger.Info("listening", "addr", ln.Addr().String(), "products", len(mustList(products)),
		"coupons", coupons.Len(), "api_keys", keys.Len())
	if ready != nil {
		ready <- ln.Addr().String()
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down: finishing requests in progress")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("stopped")
	return nil
}

func mustList(r *product.MemoryRepository) []product.Product {
	list, _ := r.List(context.Background()) // the in-memory menu never fails
	return list
}
