// Package order places orders. It checks each line against the menu and the
// limits, validates the coupon, and saves the order with a snapshot of the
// products as they were when it was placed.
package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/product"
)

// Line is one product and how many of it.
type Line struct {
	ProductID int64
	Quantity  int
}

// Request is an order as a client asked for it, after the HTTP layer has
// read the JSON. CouponCode is empty when no coupon was sent.
type Request struct {
	Lines      []Line
	CouponCode string
}

// Order is a placed order.
type Order struct {
	ID    string
	Lines []Line // duplicate products merged, in the order first seen
	// Products is a copy of each ordered product at the time of the order,
	// one per line. Prices and names on the menu may change later; the order
	// keeps what the customer agreed to.
	Products   []product.Product
	CouponCode string // the validated code, or empty
	CreatedAt  time.Time
}

// Limits bound the size of an order. They are settings, not constants.
type Limits struct {
	MaxLines    int // lines in the request, before merging
	MaxQuantity int // per product, after merging
}

// DefaultLimits suit a food order: more than 50 different lines or more
// than 100 of one item is almost certainly a mistake or abuse.
var DefaultLimits = Limits{MaxLines: 50, MaxQuantity: 100}

// ValidationError means the request was well-formed but broke a rule. The
// API answers 422 and passes Type to the client.
type ValidationError struct {
	Type    string // stable, machine-readable reason, such as "unknown_product"
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(errType, format string, args ...any) error {
	return &ValidationError{Type: errType, Message: fmt.Sprintf(format, args...)}
}

// Repository saves orders. It is an interface so a database can replace the
// in-memory version.
type Repository interface {
	Save(ctx context.Context, o Order) error
}

// Service places orders.
type Service struct {
	Products product.Repository
	Coupons  *coupon.Validator
	Orders   Repository
	Limits   Limits

	NewID func() string    // defaults to a random UUID; tests can fix it
	Now   func() time.Time // defaults to time.Now
}

// Place checks req and saves it as an order. Cheap checks come first. It
// returns a *ValidationError when a rule is broken, an error wrapping
// coupon.ErrUnavailable when the coupon could not be checked, and any other
// error for an unexpected failure. Nothing is saved unless every check passes.
func (s *Service) Place(ctx context.Context, req Request) (Order, error) {
	lines, err := s.mergeLines(req.Lines)
	if err != nil {
		return Order{}, err
	}

	// Every product must exist: an unknown product rejects the whole order.
	products := make([]product.Product, len(lines))
	var unknown []string
	for i, l := range lines {
		p, err := s.Products.Get(ctx, l.ProductID)
		switch {
		case errors.Is(err, product.ErrNotFound):
			unknown = append(unknown, fmt.Sprint(l.ProductID))
		case err != nil:
			return Order{}, fmt.Errorf("looking up product %d: %w", l.ProductID, err)
		}
		products[i] = p
	}
	if len(unknown) > 0 {
		return Order{}, invalid("unknown_product", "unknown product ID: %s", strings.Join(unknown, ", "))
	}

	// No coupon (absent, empty or spaces only) is fine. A coupon that was
	// sent must be valid; if it cannot be checked, the order is not accepted.
	code := ""
	if strings.TrimSpace(req.CouponCode) != "" {
		code, err = s.Coupons.Validate(ctx, req.CouponCode)
		switch {
		case errors.Is(err, coupon.ErrInvalid):
			return Order{}, invalid("invalid_coupon", "invalid coupon code")
		case err != nil:
			return Order{}, err
		}
	}

	o := Order{
		ID:         s.newID(),
		Lines:      lines,
		Products:   products,
		CouponCode: code,
		CreatedAt:  s.now(),
	}
	if err := s.Orders.Save(ctx, o); err != nil {
		return Order{}, fmt.Errorf("saving order: %w", err)
	}
	return o, nil
}

// mergeLines applies the limits and merges lines for the same product,
// keeping the order in which products first appear.
func (s *Service) mergeLines(in []Line) ([]Line, error) {
	if len(in) == 0 {
		return nil, invalid("no_items", "an order needs at least one item")
	}
	if len(in) > s.Limits.MaxLines {
		return nil, invalid("too_many_items", "an order can have at most %d lines, got %d", s.Limits.MaxLines, len(in))
	}
	index := make(map[int64]int, len(in)) // product ID -> position in merged
	var merged []Line
	for _, l := range in {
		if l.Quantity < 1 {
			return nil, invalid("invalid_quantity", "quantity for product %d must be at least 1, got %d", l.ProductID, l.Quantity)
		}
		if i, seen := index[l.ProductID]; seen {
			merged[i].Quantity += l.Quantity
		} else {
			index[l.ProductID] = len(merged)
			merged = append(merged, l)
		}
	}
	for _, l := range merged {
		if l.Quantity > s.Limits.MaxQuantity {
			return nil, invalid("invalid_quantity", "quantity for product %d can be at most %d, got %d", l.ProductID, s.Limits.MaxQuantity, l.Quantity)
		}
	}
	return merged, nil
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return NewUUID()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
