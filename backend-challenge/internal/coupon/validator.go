package coupon

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalid means the code is not a valid coupon. The API answers 422.
	ErrInvalid = errors.New("invalid coupon code")

	// ErrUnavailable means a check could not be completed, for example
	// because the coupon store is down. The API answers 503: a coupon that
	// could not be checked is never accepted.
	ErrUnavailable = errors.New("coupon check unavailable")
)

// Check is one step in validating a coupon. It returns nil to let the code
// through, an error wrapping ErrInvalid to reject it, or an error wrapping
// ErrUnavailable if it could not decide.
type Check func(ctx context.Context, code string) error

// Validator runs its checks in order and stops at the first failure, so
// cheap checks go first. New coupon rules, such as expiry dates, usage
// limits or a minimum order value, are new checks; nothing else changes.
type Validator struct {
	checks []Check
}

// NewValidator returns a validator that runs checks in the order given.
func NewValidator(checks ...Check) *Validator {
	return &Validator{checks: checks}
}

// DefaultValidator returns the assignment's validator: the length rule
// first, because it is free, then a lookup in the published list.
func DefaultValidator(rule Rule, store Store) *Validator {
	return NewValidator(LengthCheck(rule), MembershipCheck(store))
}

// Validate checks a code a customer submitted and returns it as checked.
//
// Only surrounding spaces are removed first, because forms and copy-paste
// add them. Nothing else about the code is changed: codes are matched
// exactly, case included, the same way the ingest tool reads them.
func (v *Validator) Validate(ctx context.Context, input string) (string, error) {
	code := strings.TrimSpace(input)
	for _, check := range v.checks {
		if err := check(ctx, code); err != nil {
			return "", err
		}
	}
	return code, nil
}

// LengthCheck rejects codes outside the rule's length range.
func LengthCheck(rule Rule) Check {
	return func(_ context.Context, code string) error {
		if !rule.LengthOK(code) {
			return fmt.Errorf("%w: must be %d to %d characters", ErrInvalid, rule.MinLen, rule.MaxLen)
		}
		return nil
	}
}

// MembershipCheck rejects codes that are not in the store. If the store
// fails, the code is not accepted: the error wraps ErrUnavailable.
func MembershipCheck(store Store) Check {
	return func(ctx context.Context, code string) error {
		ok, err := store.Contains(ctx, code)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if !ok {
			return ErrInvalid
		}
		return nil
	}
}
