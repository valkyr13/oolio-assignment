package coupon

import (
	"context"
	"errors"
	"testing"
)

// failingStore is a store whose backend is down.
type failingStore struct{}

func (failingStore) Contains(context.Context, string) (bool, error) {
	return false, errors.New("connection refused")
}

// countingStore records how many lookups reach it.
type countingStore struct {
	Store
	calls int
}

func (s *countingStore) Contains(ctx context.Context, code string) (bool, error) {
	s.calls++
	return s.Store.Contains(ctx, code)
}

func TestDefaultValidator(t *testing.T) {
	v := DefaultValidator(DefaultRule, NewMemoryStore([]string{"HAPPYHRS", "FIFTYOFF"}))
	tests := []struct {
		input    string
		wantCode string
		wantErr  error
	}{
		{"HAPPYHRS", "HAPPYHRS", nil},
		{"  FIFTYOFF \t", "FIFTYOFF", nil}, // surrounding spaces are removed
		{"happyhrs", "", ErrInvalid},       // case is not changed
		{"SUPER100", "", ErrInvalid},       // right length, not in the list
		{"SHORT", "", ErrInvalid},          // too short
		{"MUCHTOOLONG", "", ErrInvalid},    // 11 characters
		{"", "", ErrInvalid},
	}
	for _, tt := range tests {
		code, err := v.Validate(context.Background(), tt.input)
		if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
			t.Errorf("Validate(%q) error = %v, want %v", tt.input, err, tt.wantErr)
		}
		if code != tt.wantCode {
			t.Errorf("Validate(%q) code = %q, want %q", tt.input, code, tt.wantCode)
		}
	}
}

func TestStoreFailureIsNeverAccepted(t *testing.T) {
	v := DefaultValidator(DefaultRule, failingStore{})
	_, err := v.Validate(context.Background(), "HAPPYHRS")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if errors.Is(err, ErrInvalid) {
		t.Fatal("a store failure must not look like an invalid coupon")
	}
}

func TestLengthIsCheckedBeforeTheStore(t *testing.T) {
	store := &countingStore{Store: NewMemoryStore([]string{"HAPPYHRS"})}
	v := DefaultValidator(DefaultRule, store)
	v.Validate(context.Background(), "SHORT")
	if store.calls != 0 {
		t.Fatalf("store was called %d times for a code of the wrong length", store.calls)
	}
}

func TestNewRulesAreExtraChecks(t *testing.T) {
	// A future rule, for example a code that has been withdrawn, is one more
	// check in the chain.
	withdrawn := func(_ context.Context, code string) error {
		if code == "FIFTYOFF" {
			return ErrInvalid
		}
		return nil
	}
	store := NewMemoryStore([]string{"HAPPYHRS", "FIFTYOFF"})
	v := NewValidator(LengthCheck(DefaultRule), withdrawn, MembershipCheck(store))

	if _, err := v.Validate(context.Background(), "HAPPYHRS"); err != nil {
		t.Errorf("HAPPYHRS: %v", err)
	}
	if _, err := v.Validate(context.Background(), "FIFTYOFF"); !errors.Is(err, ErrInvalid) {
		t.Errorf("FIFTYOFF: error = %v, want ErrInvalid", err)
	}
}
