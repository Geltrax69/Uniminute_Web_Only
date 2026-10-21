package site

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"lamazon/website/shop"
)

type paymentControlRequest struct {
	Operation   string `json:"operation"`
	Account     string `json:"account"`
	Fingerprint string `json:"fingerprint"`
	CartID      string `json:"cart_id"`
	Amount      int64  `json:"amount"`
	Lease       string `json:"lease"`
	OrderID     string `json:"order_id"`
	Timestamp   int64  `json:"timestamp"`
}
type paymentReservation struct {
	State   string `json:"state"`
	OrderID string `json:"order_id"`
	Receipt string `json:"receipt"`
	Lease   string `json:"lease"`
	CartID  string `json:"cart_id"`
}
type paymentControlError struct {
	status         int
	message, retry string
}

func (e *paymentControlError) Error() string { return e.message }

func (s *Site) paymentControl(ctx context.Context, in paymentControlRequest) (paymentReservation, error) {
	var out paymentReservation
	in.Timestamp = time.Now().Unix()
	body, _ := json.Marshal(in)
	mac := hmac.New(sha256.New, []byte(s.razorpayKeySecret))
	mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.apiBase, "/")+"/api/payments/control", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uniminute-Payment-Signature", hex.EncodeToString(mac.Sum(nil)))
	client := http.Client{Timeout: 4 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8192))
	if err != nil {
		return out, err
	}
	if res.StatusCode != 200 {
		var msg struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &msg)
		if msg.Error == "" {
			msg.Error = "Payment protection is temporarily unavailable. Try again shortly."
		}
		return out, &paymentControlError{res.StatusCode, msg.Error, res.Header.Get("Retry-After")}
	}
	if err = json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func paymentControlFailure(w http.ResponseWriter, err error) {
	var e *paymentControlError
	if errors.As(err, &e) {
		if e.retry != "" {
			w.Header().Set("Retry-After", e.retry)
		}
		paymentError(w, e.status, e.message)
	} else {
		paymentError(w, 503, "Could not check payment status. Please wait a moment and try again.")
	}
}

// Include basket revision, shopper, destination, verified amount and API key.
// A timestamp/receipt supplied by the browser must never defeat reuse.
func paymentBasket(r *http.Request, account, address, keyID string, amount int64) (fingerprint, cartID string) {
	lines := shop.ReadCart(r)
	sort.Slice(lines, func(i, j int) bool { return lines[i].ID < lines[j].ID })
	body, _ := json.Marshal(struct {
		Account, Address, Key string
		Amount                int64
		Lines                 []shop.CartLine
	}{account, address, keyID, amount, lines})
	sum := sha256.Sum256(body)
	cartID = hex.EncodeToString(sum[:16])
	if c, err := r.Cookie(shop.CartRIDCookie); err == nil && len(c.Value) >= 8 && len(c.Value) <= 100 {
		cartID = c.Value
	}
	sum = sha256.Sum256(append(body, []byte(":"+cartID)...))
	return hex.EncodeToString(sum[:]), cartID
}

type razorpayOrderLookup interface {
	All(map[string]interface{}, map[string]string) (map[string]interface{}, error)
	Fetch(string, map[string]interface{}, map[string]string) (map[string]interface{}, error)
	Payments(string, map[string]interface{}, map[string]string) (map[string]interface{}, error)
}

func (s *Site) reservedRazorpayOrder(ctx context.Context, in paymentControlRequest, res paymentReservation) (map[string]interface{}, error) {
	lookup, ok := s.razorpay.(razorpayOrderLookup)
	if !ok {
		return nil, errors.New("payment lookup unavailable")
	}
	switch res.State {
	case "reuse":
		return lookup.Fetch(res.OrderID, nil, nil)
	case "recover":
		// A network error does not prove creation failed. Reconcile by the original
		// server receipt; never automatically create a second order after a timeout.
		result, err := lookup.All(map[string]interface{}{"receipt": res.Receipt, "count": 100}, nil)
		if err != nil {
			return nil, err
		}
		items, _ := result["items"].([]interface{})
		var found map[string]interface{}
		for _, item := range items {
			order, _ := item.(map[string]interface{})
			if order["receipt"] == res.Receipt {
				if found != nil {
					return nil, errors.New("multiple matching payment attempts")
				}
				found = order
			}
		}
		if found == nil {
			return nil, errors.New("previous payment attempt has not been confirmed")
		}
		return found, nil
	case "create":
		return s.razorpay.Create(map[string]interface{}{"amount": in.Amount, "currency": razorpayCurrency, "receipt": res.Receipt}, nil)
	default:
		return nil, fmt.Errorf("invalid reservation state")
	}
}

// Only captured payments can resume confirmation. An authorised/pending payment
// must settle first; failed payments may safely retry the same provider order.
func (s *Site) existingRazorpayPayment(orderID string, amount int64) (*verifyPaymentRequest, bool, error) {
	lookup, ok := s.razorpay.(razorpayOrderLookup)
	if !ok {
		return nil, false, errors.New("payment lookup unavailable")
	}
	result, err := lookup.Payments(orderID, nil, nil)
	if err != nil {
		return nil, false, err
	}
	items, ok := result["items"].([]interface{})
	if !ok || len(items) == 0 {
		return nil, true, nil
	}
	pending := false
	var captured *verifyPaymentRequest
	for _, item := range items {
		payment, _ := item.(map[string]interface{})
		status, _ := payment["status"].(string)
		if status == "failed" {
			continue
		}
		n, valid := integerValue(payment["amount"])
		id, _ := payment["id"].(string)
		if status == "captured" && valid && n == amount && payment["currency"] == razorpayCurrency && payment["order_id"] == orderID && id != "" {
			if captured != nil {
				return nil, true, errors.New("multiple captured payments")
			}
			mac := hmac.New(sha256.New, []byte(s.razorpayKeySecret))
			mac.Write([]byte(orderID + "|" + id))
			captured = &verifyPaymentRequest{PaymentID: id, OrderID: orderID, Signature: hex.EncodeToString(mac.Sum(nil))}
		} else {
			pending = true
		}
	}
	return captured, pending, nil
}
