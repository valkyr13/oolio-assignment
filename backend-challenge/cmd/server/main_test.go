package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.OrderMaxLines != 50 || cfg.OrderMaxQuantity != 100 || cfg.MaxBodyBytes != 16384 {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoadConfigRejectsBadNumbers(t *testing.T) {
	for _, bad := range []map[string]string{
		{"ORDER_MAX_LINES": "lots"},
		{"ORDER_MAX_QUANTITY": "0"},
		{"ORDER_MAX_BODY_BYTES": "-5"},
		{"LOG_FORMAT": "xml"},
	} {
		if _, err := loadConfig(env(bad)); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}

// TestServerEndToEnd starts the real server on a free port, places an order
// over HTTP, then shuts it down the way Docker does.
func TestServerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	coupons := filepath.Join(dir, "valid.txt")
	os.WriteFile(coupons, []byte("HAPPYHRS\nFIFTYOFF\n"), 0o644)
	cfg, err := loadConfig(env(map[string]string{
		"ADDR":          "127.0.0.1:0",
		"API_KEYS":      "apitest:create_order",
		"PRODUCTS_FILE": filepath.Join("..", "..", "data", "products.json"),
		"COUPONS_FILE":  coupons,
	}))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), ready) }()

	var base string
	select {
	case addr := <-ready:
		base = "http://" + addr
	case err := <-done:
		t.Fatalf("server did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start in time")
	}

	resp, err := http.Get(base + "/api/product/1")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /api/product/1: %v %v", resp, err)
	}
	resp.Body.Close()

	req, _ := http.NewRequest("POST", base+"/api/order",
		strings.NewReader(`{"couponCode":"HAPPYHRS","items":[{"productId":"1","quantity":2}]}`))
	req.Header.Set("api_key", "apitest")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("POST /api/order: %v %v", resp, err)
	}
	resp.Body.Close()

	cancel() // what SIGTERM does
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestStartupFailsOnMissingCoupons(t *testing.T) {
	cfg, _ := loadConfig(env(map[string]string{
		"ADDR":          "127.0.0.1:0",
		"PRODUCTS_FILE": filepath.Join("..", "..", "data", "products.json"),
		"COUPONS_FILE":  filepath.Join(t.TempDir(), "missing.txt"),
	}))
	err := run(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err == nil || !strings.Contains(err.Error(), "coupons") {
		t.Fatalf("error = %v, want a clear error about the coupons file", err)
	}
}
