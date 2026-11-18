package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/valkyr13/oolio-assignment/internal/coupon"
	"github.com/valkyr13/oolio-assignment/internal/order"
)

// orderRequestJSON is the spec's OrderReq schema. Pointers tell a missing
// field apart from a zero value: a missing required field is a 400, while a
// present but unacceptable value (quantity 0) is a 422.
type orderRequestJSON struct {
	CouponCode string          `json:"couponCode"`
	Items      []orderItemJSON `json:"items"`
}

type orderItemJSON struct {
	ProductID *string `json:"productId"`
	Quantity  *int    `json:"quantity"`
}

// orderJSON is the spec's Order schema.
type orderJSON struct {
	ID       string             `json:"id"`
	Items    []orderItemOutJSON `json:"items"`
	Products []productJSON      `json:"products"`
}

type orderItemOutJSON struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
}

func (s *server) placeOrder(w http.ResponseWriter, r *http.Request) {
	req, status, errType, msg := s.readOrderRequest(w, r)
	if status != 0 {
		writeError(w, status, errType, msg)
		return
	}

	o, err := s.orders.Place(r.Context(), req)
	var invalid *order.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Type, invalid.Message)
		return
	case errors.Is(err, coupon.ErrUnavailable):
		s.logger.Warn("coupon check unavailable", "error", err)
		writeError(w, http.StatusServiceUnavailable, errCouponUnavailable,
			"The coupon could not be checked; try again, or order without it")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}

	out := orderJSON{
		ID:       o.ID,
		Items:    make([]orderItemOutJSON, len(o.Lines)),
		Products: make([]productJSON, len(o.Products)),
	}
	for i, l := range o.Lines {
		out.Items[i] = orderItemOutJSON{ProductID: strconv.FormatInt(l.ProductID, 10), Quantity: l.Quantity}
	}
	for i, p := range o.Products {
		out.Products[i] = toProductJSON(p)
	}
	writeJSON(w, http.StatusOK, out)
}

// readOrderRequest reads and checks the shape of the request body. It returns
// a non-zero status, with an error type and message, if the body is not a
// well-formed OrderReq. Business rules are checked later, by order.Service.
func (s *server) readOrderRequest(w http.ResponseWriter, r *http.Request) (order.Request, int, string, string) {
	body := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	dec := json.NewDecoder(body)

	var in orderRequestJSON
	if err := dec.Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return order.Request{}, http.StatusRequestEntityTooLarge, errPayloadTooLarge,
				fmt.Sprintf("The request body can be at most %d bytes", s.maxBodyBytes)
		}
		return order.Request{}, http.StatusBadRequest, errInvalidInput, "Invalid input: " + describeJSONError(err)
	}
	// Anything after the JSON object, such as a second object, is malformed.
	if _, err := dec.Token(); err != io.EOF {
		return order.Request{}, http.StatusBadRequest, errInvalidInput, "Invalid input: unexpected data after the JSON object"
	}

	if in.Items == nil {
		return order.Request{}, http.StatusBadRequest, errInvalidInput, "Invalid input: items is required"
	}
	req := order.Request{CouponCode: in.CouponCode, Lines: make([]order.Line, 0, len(in.Items))}
	for i, item := range in.Items {
		if item.ProductID == nil || item.Quantity == nil {
			return order.Request{}, http.StatusBadRequest, errInvalidInput,
				fmt.Sprintf("Invalid input: items[%d] needs productId and quantity", i)
		}
		id, ok := parseID(*item.ProductID)
		if !ok {
			// A string, as the spec asks, but one that can never be a product.
			return order.Request{}, http.StatusUnprocessableEntity, "unknown_product",
				fmt.Sprintf("unknown product ID: %q", *item.ProductID)
		}
		req.Lines = append(req.Lines, order.Line{ProductID: id, Quantity: *item.Quantity})
	}
	return req, 0, "", ""
}

// describeJSONError turns a decoding error into a short message that names
// the problem without echoing large parts of the body back.
func describeJSONError(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Sprintf("%s must be a %s", typeErr.Field, typeErr.Type)
	case errors.As(err, &syntaxErr):
		return fmt.Sprintf("malformed JSON at byte %d", syntaxErr.Offset)
	case errors.Is(err, io.EOF):
		return "the request body is empty"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "the JSON ends too early"
	default:
		return "the body is not a valid JSON object"
	}
}
