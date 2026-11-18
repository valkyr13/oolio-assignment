package api

import (
	"encoding/json"
	"net/http"
)

// apiResponse is the spec's ApiResponse schema. Every error the server
// returns has this shape, so clients only need to handle one error format.
//
//	code     the HTTP status, repeated so it survives logging and proxies
//	type     a stable, machine-readable reason; clients branch on this
//	message  a human-readable explanation; it may change wording at any time
type apiResponse struct {
	Code    int32  `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Error types. They are part of the API contract: once published, a type
// keeps its meaning.
// Rule failures from order.Service (422) carry their own types, such as
// "no_items", "invalid_quantity", "unknown_product" and "invalid_coupon".
const (
	errInvalidID         = "invalid_id"         // 400
	errInvalidInput      = "invalid_input"      // 400
	errUnauthorized      = "unauthorized"       // 401
	errForbidden         = "forbidden"          // 403
	errNotFound          = "not_found"          // 404
	errMethodNotAllowed  = "method_not_allowed" // 405
	errPayloadTooLarge   = "payload_too_large"  // 413
	errInternal          = "internal_error"     // 500
	errCouponUnavailable = "coupon_unavailable" // 503
)

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, apiResponse{Code: int32(status), Type: errType, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// An encoding error here means the client has gone away; the status is
	// already sent, so there is nothing more useful to do.
	_ = json.NewEncoder(w).Encode(v)
}
