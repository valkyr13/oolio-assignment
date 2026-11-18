package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/valkyr13/oolio-assignment/internal/auth"
	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/order"
)

type unreachableCoupons struct{}

func (unreachableCoupons) Contains(context.Context, string) (bool, error) {
	return false, errors.New("coupon service unreachable")
}

// newAPI builds the full handler with an in-memory menu, coupons and orders.
func newAPI(t *testing.T, store coupon.Store) (http.Handler, *order.MemoryRepository) {
	t.Helper()
	keys, err := auth.ParseKeys("apitest:create_order,readonly:")
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		store = coupon.NewMemoryStore([]string{"HAPPYHRS", "FIFTYOFF"})
	}
	orders := order.NewMemoryRepository()
	svc := &order.Service{
		Products: testMenu(t),
		Coupons:  coupon.DefaultValidator(coupon.DefaultRule, store),
		Orders:   orders,
		Limits:   order.DefaultLimits,
	}
	return NewHandler(Config{Products: testMenu(t), Orders: svc, Keys: keys}), orders
}

var withKey = map[string]string{"api_key": "apitest", "Content-Type": "application/json"}

func TestPlaceOrder(t *testing.T) {
	h, orders := newAPI(t, nil)
	rec := do(h, "POST", "/api/order",
		`{"couponCode":"HAPPYHRS","items":[{"productId":"7","quantity":2},{"productId":"1","quantity":1},{"productId":"7","quantity":1}]}`,
		withKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got orderJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Error("the order has no id")
	}
	// Duplicate lines merged; products are a snapshot, one per line.
	if len(got.Items) != 2 || got.Items[0] != (orderItemOutJSON{ProductID: "7", Quantity: 3}) {
		t.Errorf("items = %+v, want product 7 x3 then product 1 x1", got.Items)
	}
	if len(got.Products) != 2 || got.Products[0].Name != "Red Velvet Cake" || got.Products[0].Price != 12.34 {
		t.Errorf("products = %+v", got.Products)
	}
	if orders.Len() != 1 {
		t.Errorf("saved %d orders, want 1", orders.Len())
	}
}

func TestOrderErrors(t *testing.T) {
	big := `{"items":[` + strings.Repeat(`{"productId":"1","quantity":1},`, 1000) + `{"productId":"1","quantity":1}]}`
	tests := []struct {
		name    string
		header  map[string]string
		body    string
		status  int
		errType string
	}{
		{"no api_key", nil, `{"items":[{"productId":"1","quantity":1}]}`, 401, errUnauthorized},
		{"unknown api_key", map[string]string{"api_key": "guess"}, `{"items":[{"productId":"1","quantity":1}]}`, 401, errUnauthorized},
		{"key without create_order", map[string]string{"api_key": "readonly"}, `{"items":[{"productId":"1","quantity":1}]}`, 403, errForbidden},

		{"empty body", withKey, ``, 400, errInvalidInput},
		{"malformed JSON", withKey, `{"items":[`, 400, errInvalidInput},
		{"not an object", withKey, `[1,2,3]`, 400, errInvalidInput},
		{"items missing", withKey, `{"couponCode":"HAPPYHRS"}`, 400, errInvalidInput},
		{"items null", withKey, `{"items":null}`, 400, errInvalidInput},
		{"productId missing", withKey, `{"items":[{"quantity":1}]}`, 400, errInvalidInput},
		{"quantity missing", withKey, `{"items":[{"productId":"1"}]}`, 400, errInvalidInput},
		{"productId as a number", withKey, `{"items":[{"productId":1,"quantity":1}]}`, 400, errInvalidInput},
		{"quantity as a string", withKey, `{"items":[{"productId":"1","quantity":"2"}]}`, 400, errInvalidInput},
		{"quantity with decimals", withKey, `{"items":[{"productId":"1","quantity":1.5}]}`, 400, errInvalidInput},
		{"two JSON objects", withKey, `{"items":[{"productId":"1","quantity":1}]}{}`, 400, errInvalidInput},
		{"body over 16 KB", withKey, big, 413, errPayloadTooLarge},

		{"empty items", withKey, `{"items":[]}`, 422, "no_items"},
		{"quantity 0", withKey, `{"items":[{"productId":"1","quantity":0}]}`, 422, "invalid_quantity"},
		{"quantity 101", withKey, `{"items":[{"productId":"1","quantity":101}]}`, 422, "invalid_quantity"},
		{"unknown product", withKey, `{"items":[{"productId":"999","quantity":1}]}`, 422, "unknown_product"},
		{"productId that can't exist", withKey, `{"items":[{"productId":"abc","quantity":1}]}`, 422, "unknown_product"},
		{"invalid coupon", withKey, `{"couponCode":"SUPER100","items":[{"productId":"1","quantity":1}]}`, 422, "invalid_coupon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, orders := newAPI(t, nil)
			wantError(t, do(h, "POST", "/api/order", tt.body, tt.header), tt.status, tt.errType)
			if orders.Len() != 0 {
				t.Fatal("a rejected order was saved")
			}
		})
	}
}

func TestOrderWithoutCouponAndUnknownFields(t *testing.T) {
	h, _ := newAPI(t, nil)
	// No coupon is fine, an empty coupon is "no coupon", and unknown fields are ignored.
	for _, body := range []string{
		`{"items":[{"productId":"1","quantity":1}]}`,
		`{"couponCode":"","items":[{"productId":"1","quantity":1}]}`,
		`{"items":[{"productId":"1","quantity":1,"note":"no nuts"}],"tableNumber":4}`,
	} {
		if rec := do(h, "POST", "/api/order", body, withKey); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, body %s", body, rec.Code, rec.Body)
		}
	}
}

func TestCouponStoreDownFailsClosed(t *testing.T) {
	h, orders := newAPI(t, unreachableCoupons{})
	rec := do(h, "POST", "/api/order", `{"couponCode":"HAPPYHRS","items":[{"productId":"1","quantity":1}]}`, withKey)
	wantError(t, rec, 503, errCouponUnavailable)
	if orders.Len() != 0 {
		t.Fatal("an order with an unchecked coupon was saved")
	}
	// Orders without a coupon still work.
	if rec := do(h, "POST", "/api/order", `{"items":[{"productId":"1","quantity":1}]}`, withKey); rec.Code != 200 {
		t.Fatalf("an order without a coupon got %d", rec.Code)
	}
}

func TestWrongMethodIsA405(t *testing.T) {
	h, _ := newAPI(t, nil)
	for _, tt := range []struct{ method, path, allow string }{
		{"GET", "/api/order", "POST"},
		{"POST", "/api/product", "GET"},
		{"DELETE", "/api/product/1", "GET"},
	} {
		rec := do(h, tt.method, tt.path, "", withKey)
		wantError(t, rec, 405, errMethodNotAllowed)
		if got := rec.Header().Get("Allow"); got != tt.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tt.method, tt.path, got, tt.allow)
		}
	}
}

func TestHealthz(t *testing.T) {
	h, _ := newAPI(t, nil)
	if rec := do(h, "GET", "/healthz", "", nil); rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
}
