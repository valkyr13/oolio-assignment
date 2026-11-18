package order

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"

	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/product"
)

type downStore struct{}

func (downStore) Contains(context.Context, string) (bool, error) {
	return false, errors.New("coupon service unreachable")
}

func newService(t *testing.T, store coupon.Store) (*Service, *MemoryRepository) {
	t.Helper()
	menu, err := product.NewMemoryRepository([]product.Product{
		{ID: 1, Name: "Waffle with Berries", Category: "Waffle", PriceMinor: 650},
		{ID: 2, Name: "Classic Tiramisu", Category: "Tiramisu", PriceMinor: 550},
	})
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		store = coupon.NewMemoryStore([]string{"HAPPYHRS", "FIFTYOFF"})
	}
	orders := NewMemoryRepository()
	return &Service{
		Products: menu,
		Coupons:  coupon.DefaultValidator(coupon.DefaultRule, store),
		Orders:   orders,
		Limits:   DefaultLimits,
	}, orders
}

// wantInvalid checks that err is a ValidationError of the given type.
func wantInvalid(t *testing.T, err error, errType string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) || v.Type != errType {
		t.Fatalf("error = %v, want a ValidationError of type %q", err, errType)
	}
}

func TestPlaceOrder(t *testing.T) {
	s, orders := newService(t, nil)
	o, err := s.Place(context.Background(), Request{
		Lines:      []Line{{ProductID: 2, Quantity: 1}, {ProductID: 1, Quantity: 2}, {ProductID: 2, Quantity: 3}},
		CouponCode: "  HAPPYHRS ",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate lines are merged, keeping the order products first appear in.
	want := []Line{{ProductID: 2, Quantity: 4}, {ProductID: 1, Quantity: 2}}
	if fmt.Sprint(o.Lines) != fmt.Sprint(want) {
		t.Errorf("lines = %v, want %v", o.Lines, want)
	}
	if len(o.Products) != 2 || o.Products[0].ID != 2 || o.Products[1].PriceMinor != 650 {
		t.Errorf("products = %+v, want a snapshot of products 2 and 1", o.Products)
	}
	if o.CouponCode != "HAPPYHRS" {
		t.Errorf("coupon = %q, want HAPPYHRS", o.CouponCode)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(o.ID) {
		t.Errorf("ID %q is not a version 4 UUID", o.ID)
	}
	if saved, ok := orders.Get(context.Background(), o.ID); !ok || saved.ID != o.ID {
		t.Error("the order was not saved")
	}
}

func TestRulesAreChecked(t *testing.T) {
	many := make([]Line, 51)
	for i := range many {
		many[i] = Line{ProductID: 1, Quantity: 1}
	}
	tests := []struct {
		name    string
		req     Request
		errType string
	}{
		{"no lines", Request{}, "no_items"},
		{"51 lines", Request{Lines: many}, "too_many_items"},
		{"quantity 0", Request{Lines: []Line{{ProductID: 1, Quantity: 0}}}, "invalid_quantity"},
		{"negative quantity", Request{Lines: []Line{{ProductID: 1, Quantity: -2}}}, "invalid_quantity"},
		{"101 of one item", Request{Lines: []Line{{ProductID: 1, Quantity: 101}}}, "invalid_quantity"},
		{"over the limit once merged", Request{Lines: []Line{{ProductID: 1, Quantity: 60}, {ProductID: 1, Quantity: 41}}}, "invalid_quantity"},
		{"unknown product", Request{Lines: []Line{{ProductID: 1, Quantity: 1}, {ProductID: 99, Quantity: 1}}}, "unknown_product"},
		{"invalid coupon", Request{Lines: []Line{{ProductID: 1, Quantity: 1}}, CouponCode: "SUPER100"}, "invalid_coupon"},
		{"coupon in the wrong case", Request{Lines: []Line{{ProductID: 1, Quantity: 1}}, CouponCode: "happyhrs"}, "invalid_coupon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, orders := newService(t, nil)
			_, err := s.Place(context.Background(), tt.req)
			wantInvalid(t, err, tt.errType)
			if orders.Len() != 0 {
				t.Fatal("a rejected order was saved")
			}
		})
	}
}

func TestNoCouponIsFine(t *testing.T) {
	for _, code := range []string{"", "   "} {
		s, _ := newService(t, nil)
		o, err := s.Place(context.Background(), Request{Lines: []Line{{ProductID: 1, Quantity: 1}}, CouponCode: code})
		if err != nil || o.CouponCode != "" {
			t.Errorf("coupon %q: order %+v, error %v; want an order with no coupon", code, o, err)
		}
	}
}

func TestCouponStoreDown(t *testing.T) {
	s, orders := newService(t, downStore{})
	lines := []Line{{ProductID: 1, Quantity: 1}}

	// With a coupon, the order fails closed: it is not accepted unchecked.
	_, err := s.Place(context.Background(), Request{Lines: lines, CouponCode: "HAPPYHRS"})
	if !errors.Is(err, coupon.ErrUnavailable) {
		t.Fatalf("error = %v, want coupon.ErrUnavailable", err)
	}
	if orders.Len() != 0 {
		t.Fatal("an order with an unchecked coupon was saved")
	}
	// Without a coupon, the order still goes through.
	if _, err := s.Place(context.Background(), Request{Lines: lines}); err != nil {
		t.Fatalf("an order without a coupon failed: %v", err)
	}
}

func TestConcurrentOrders(t *testing.T) {
	s, orders := newService(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Place(context.Background(), Request{Lines: []Line{{ProductID: 1, Quantity: 1}}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if orders.Len() != 100 {
		t.Fatalf("saved %d orders, want 100", orders.Len())
	}
}

func TestDuplicateIDIsRejected(t *testing.T) {
	repo := NewMemoryRepository()
	o := Order{ID: "same"}
	if err := repo.Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), o); err == nil {
		t.Fatal("saving the same ID twice should fail")
	}
}
