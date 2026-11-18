package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/valkyr13/oolio-assignment/internal/product"
)

func testMenu(t *testing.T) product.Repository {
	t.Helper()
	repo, err := product.NewMemoryRepository([]product.Product{
		{ID: 1, Name: "Waffle with Berries", Category: "Waffle", PriceMinor: 650},
		{ID: 7, Name: "Red Velvet Cake", Category: "Cake", PriceMinor: 1234},
	})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// do sends a request to a handler and returns the recorded response.
func do(h http.Handler, method, path string, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// wantError checks that a response is an ApiResponse with the given status and type.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, errType string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not an ApiResponse: %s", rec.Body)
	}
	if int(got.Code) != status || got.Type != errType || got.Message == "" {
		t.Fatalf("error body = %+v, want code %d and type %q with a message", got, status, errType)
	}
}

func TestListProducts(t *testing.T) {
	h := NewHandler(Config{Products: testMenu(t)})
	rec := do(h, "GET", "/api/product", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []productJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []productJSON{
		{ID: "1", Name: "Waffle with Berries", Price: 6.5, Category: "Waffle"},
		{ID: "7", Name: "Red Velvet Cake", Price: 12.34, Category: "Cake"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("products = %+v, want %+v", got, want)
	}
	// The price is written as a plain decimal number, as the spec defines.
	if !strings.Contains(rec.Body.String(), `"price":12.34`) {
		t.Errorf("body %s does not contain \"price\":12.34", rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != menuCacheControl {
		t.Errorf("Cache-Control = %q, want %q", cc, menuCacheControl)
	}
}

func TestEmptyMenuIsAnEmptyArray(t *testing.T) {
	empty, _ := product.NewMemoryRepository(nil)
	rec := do(NewHandler(Config{Products: empty}), "GET", "/api/product", "", nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestGetProduct(t *testing.T) {
	h := NewHandler(Config{Products: testMenu(t)})
	tests := []struct {
		path    string
		status  int
		errType string
		wantID  string
	}{
		{"/api/product/1", 200, "", "1"},
		{"/api/product/007", 200, "", "7"}, // leading zeros are allowed
		{"/api/product/abc", 400, errInvalidID, ""},
		{"/api/product/0", 400, errInvalidID, ""},
		{"/api/product/-1", 400, errInvalidID, ""},
		{"/api/product/+1", 400, errInvalidID, ""},
		{"/api/product/1.5", 400, errInvalidID, ""},
		{"/api/product/99999999999999999999", 400, errInvalidID, ""}, // too big for int64
		{"/api/product/999", 404, errNotFound, ""},                   // valid ID, no such product
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := do(h, "GET", tt.path, "", nil)
			if tt.errType != "" {
				wantError(t, rec, tt.status, tt.errType)
				return
			}
			var got productJSON
			if rec.Code != tt.status || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.ID != tt.wantID {
				t.Fatalf("status %d, body %s; want %d with id %q", rec.Code, rec.Body, tt.status, tt.wantID)
			}
		})
	}
}

func TestUnknownEndpointIsAJSON404(t *testing.T) {
	h := NewHandler(Config{Products: testMenu(t)})
	wantError(t, do(h, "GET", "/api/nothing", "", nil), 404, errNotFound)
}

type brokenMenu struct{ panics bool }

func (b brokenMenu) List(context.Context) ([]product.Product, error) {
	if b.panics {
		panic("boom")
	}
	return nil, errors.New("database password is hunter2")
}

func (b brokenMenu) Get(context.Context, int64) (product.Product, error) {
	return product.Product{}, errors.New("database password is hunter2")
}

func TestInternalErrorsDoNotLeak(t *testing.T) {
	h := NewHandler(Config{Products: brokenMenu{}})
	rec := do(h, "GET", "/api/product", "", nil)
	wantError(t, rec, 500, errInternal)
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatal("an internal error message reached the client")
	}
}

func TestPanicBecomesA500(t *testing.T) {
	h := NewHandler(Config{Products: brokenMenu{panics: true}})
	wantError(t, do(h, "GET", "/api/product", "", nil), 500, errInternal)
}
