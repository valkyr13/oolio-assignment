package api

import "strconv"

// parseID reads a product ID sent by a client.
//
// The spec types the path parameter as an int64 but Product.id as a string,
// so one rule covers both: one or more digits, no sign or spaces, from 1 up
// to the largest int64. Leading zeros are allowed, so "007" is product 7.
// Anything else can never be a valid ID, which is why it is a 400 and not
// a 404.
func parseID(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 1 { // err means the number is too large for an int64
		return 0, false
	}
	return id, true
}
