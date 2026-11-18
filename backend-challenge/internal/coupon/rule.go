// Package coupon holds the coupon rule shared by the ingest tool and the API
// server, so the two can never disagree about what a valid code looks like.
package coupon

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// DefaultRule is the rule from the assignment: a code is valid if it is
// 8 to 10 characters long and appears in at least 2 coupon files.
var DefaultRule = Rule{MinLen: 8, MaxLen: 10, MinFiles: 2}

// Rule describes what makes a coupon code valid. The values are settings,
// not constants, so the business can change them without a code change.
//
// Codes are compared exactly as written, case included. The assignment does
// not say codes are case-insensitive, so nothing here changes a code.
type Rule struct {
	MinLen   int // shortest allowed code, in characters
	MaxLen   int // longest allowed code, in characters
	MinFiles int // how many different coupon files a code must appear in
}

// Validate reports whether the rule's settings make sense.
func (r Rule) Validate() error {
	switch {
	case r.MinLen < 1:
		return errors.New("coupon rule: minimum length must be at least 1")
	case r.MaxLen < r.MinLen:
		return fmt.Errorf("coupon rule: maximum length %d is below minimum length %d", r.MaxLen, r.MinLen)
	case r.MinFiles < 1:
		return errors.New("coupon rule: minimum files must be at least 1")
	}
	return nil
}

// LengthOK reports whether code has an allowed length. Length is counted in
// characters, not bytes, to match the wording of the rule.
func (r Rule) LengthOK(code string) bool {
	n := utf8.RuneCountInString(code)
	return n >= r.MinLen && n <= r.MaxLen
}
