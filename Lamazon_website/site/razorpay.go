package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"lamazon/website/backend"
	"lamazon/website/shop"

	razorpay "github.com/razorpay/razorpay-go"
	rzperrors "github.com/razorpay/razorpay-go/errors"
)

const (
	razorpayCurrency    = "INR"
	razorpayStateCookie = "lw_rzp"
	razorpayStateTTL    = 30 * time.Minute
)

type razorpayOrderCreator interface {
	Create(map[string]interface{}, map[string]string) (map[string]interface{}, error)
}

type razorpayConfig struct {
	orders    razorpayOrderCreator
	keyID     string
	keySecret string
}

func razorpayFromEnv() razorpayConfig {
	keyID := strings.TrimSpace(os.Getenv("RAZORPAY_KEY_ID"))
	keySecret := strings.TrimSpace(os.Getenv("RAZORPAY_KEY_SECRET"))
	if keyID == "" || keySecret == "" {
		return razorpayConfig{keyID: keyID, keySecret: keySecret}
	}
	return razorpayConfig{
		orders:    razorpay.NewClient(keyID, keySecret).Order,
		keyID:     keyID,
		keySecret: keySecret,
	}
}

type createOrderRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Receipt  string `json:"receipt"`
}

type paymentState struct {
	OrderID string `json:"order_id"`
	Amount  int64  `json:"amount"`
	CartID  string `json:"cart_id"`
	Address string `json:"address_id"`
	Expires int64  `json:"expires"`
}

func (s *Site) handleCreateRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	if s.razorpay == nil || s.razorpayKeyID == "" || s.razorpayKeySecret == "" {
		if s.paymentProxy != nil {
			s.paymentProxy.ServeHTTP(w, r)
			return
		}
		paymentError(w, http.StatusServiceUnavailable, "Online payment is temporarily unavailable.")
		return
	}
	var in createOrderRequest
	if err := decodePaymentJSON(w, r, &in); err != nil {
		paymentError(w, http.StatusBadRequest, "Send a valid order request.")
		return
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Receipt = strings.TrimSpace(in.Receipt)
	if in.Amount < 100 {
		paymentError(w, http.StatusBadRequest, "Payment amount must be at least ₹1.")
		return
	}
	if in.Currency != razorpayCurrency {
		paymentError(w, http.StatusBadRequest, "Only INR payments are supported.")
		return
	}
	if in.Receipt == "" || len(in.Receipt) > 40 {
		paymentError(w, http.StatusBadRequest, "Receipt must be between 1 and 40 characters.")
		return
	}
	p := s.buildPage(r)
	if p.User == nil {
		paymentError(w, http.StatusUnauthorized, "Sign in before paying for your order.")
		return
	}
	if p.Address == nil {
		paymentError(w, http.StatusBadRequest, "Add a delivery address before paying.")
		return
	}
	amount, ok := s.cartAmountPaise(r)
	if !ok {
		paymentError(w, http.StatusBadRequest, "Your cart is empty.")
		return
	}
	if amount < 100 {
		paymentError(w, http.StatusBadRequest, "Payment amount must be at least ₹1.")
		return
	}
	if amount != in.Amount {
		paymentError(w, http.StatusBadRequest, "Your cart total changed. Refresh the cart and try again.")
		return
	}

	fingerprint, cartID := paymentBasket(r, p.User.Email, p.Address.ID, s.razorpayKeyID, amount)
	control := paymentControlRequest{Operation: "reserve", Account: p.User.Email, Fingerprint: fingerprint, CartID: cartID, Amount: amount}
	reservation, err := s.paymentControl(r.Context(), control)
	if err != nil {
		paymentControlFailure(w, err)
		return
	}
	// A brand-new payment must not start while its shop is closed or all
	// riders are off. Existing provider attempts still need recovery, and a
	// captured payment must be recorded even if availability later changes.
	if reservation.State == "create" {
		if err := s.checkNewPaymentAvailability(r); err != nil {
			control.Operation = "release"
			control.Lease = reservation.Lease
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Second)
			_, _ = s.paymentControl(ctx, control)
			cancel()
			paymentError(w, http.StatusConflict, err.Error())
			return
		}
	}
	order, err := s.reservedRazorpayOrder(r.Context(), control, reservation)
	if err != nil {
		var badRequest *rzperrors.BadRequestError
		// Provider validation rejection is definite; transport failures retain the
		// reservation because the provider could have accepted the request.
		if reservation.State == "create" && errors.As(err, &badRequest) {
			control.Operation = "release"
			control.Lease = reservation.Lease
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Second)
			_, _ = s.paymentControl(ctx, control)
			cancel()
		}
		paymentError(w, http.StatusBadGateway, "Could not confirm the payment attempt. Wait a moment and retry; your earlier attempt will be checked first.")
		return
	}

	orderID, _ := order["id"].(string)
	orderAmount, amountOK := integerValue(order["amount"])
	orderCurrency, _ := order["currency"].(string)
	if orderID == "" || !amountOK || orderAmount != amount || orderCurrency != razorpayCurrency {
		paymentError(w, http.StatusInternalServerError, "Razorpay returned an incomplete order. Try again.")
		return
	}

	control.Operation = "finish"
	control.Lease = reservation.Lease
	control.OrderID = orderID
	// Persist success even if the browser disconnected while Razorpay responded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Second)
	_, err = s.paymentControl(ctx, control)
	cancel()
	if err != nil {
		paymentControlFailure(w, err)
		return
	}

	var recovered *verifyPaymentRequest
	status, _ := order["status"].(string)
	if status == "paid" || status == "attempted" {
		var pending bool
		recovered, pending, err = s.existingRazorpayPayment(orderID, amount)
		if err != nil || (recovered == nil && (pending || status == "paid")) {
			paymentJSON(w, http.StatusConflict, map[string]any{"error": "Your earlier payment is still being checked. Check your orders and bank status, then retry confirmation. Do not pay again yet.", "payment_pending": true})
			return
		}
	}

	cartID = reservation.CartID
	shop.WriteCookie(w, shop.CartRIDCookie, cartID)

	state := paymentState{
		OrderID: orderID,
		Amount:  amount,
		CartID:  cartID,
		Address: p.Address.ID,
		Expires: time.Now().Add(razorpayStateTTL).Unix(),
	}
	if err := s.setPaymentState(w, r, state); err != nil {
		paymentError(w, http.StatusInternalServerError, "Could not secure the payment session. Try again.")
		return
	}
	paymentJSON(w, http.StatusCreated, map[string]any{
		"order_id": orderID,
		"amount":   amount,
		"currency": razorpayCurrency,
		"key_id":   s.razorpayKeyID,
		"payment":  recovered,
	})
}

type verifyPaymentRequest struct {
	PaymentID string `json:"razorpay_payment_id"`
	OrderID   string `json:"razorpay_order_id"`
	Signature string `json:"razorpay_signature"`
}

func (s *Site) handleVerifyRazorpayPayment(w http.ResponseWriter, r *http.Request) {
	if s.razorpayKeySecret == "" {
		if s.paymentProxy != nil {
			s.paymentProxy.ServeHTTP(w, r)
			return
		}
		paymentError(w, http.StatusServiceUnavailable, "Online payment is temporarily unavailable.")
		return
	}
	var in verifyPaymentRequest
	if err := decodePaymentJSON(w, r, &in); err != nil {
		paymentError(w, http.StatusBadRequest, "Send valid payment details.")
		return
	}
	in.PaymentID = strings.TrimSpace(in.PaymentID)
	in.OrderID = strings.TrimSpace(in.OrderID)
	in.Signature = strings.TrimSpace(in.Signature)
	if in.PaymentID == "" || in.OrderID == "" || in.Signature == "" {
		paymentError(w, http.StatusBadRequest, "Payment ID, order ID and signature are required.")
		return
	}
	state, err := s.readPaymentState(r)
	if err != nil || state.OrderID != in.OrderID || state.Expires < time.Now().Unix() {
		paymentError(w, http.StatusBadRequest, "This payment session expired. Return to your cart and try again.")
		return
	}
	if !verifyPaymentSignature(in.OrderID, in.PaymentID, in.Signature, s.razorpayKeySecret) {
		paymentError(w, http.StatusBadRequest, "Payment signature did not match. The order was not marked as paid.")
		return
	}
	if cartID, cookieErr := r.Cookie(shop.CartRIDCookie); cookieErr != nil || cartID.Value != state.CartID {
		paymentError(w, http.StatusBadRequest, "Your cart changed during payment. Contact support with your payment ID.")
		return
	}

	p := s.buildPage(r)
	if p.User == nil {
		paymentError(w, http.StatusUnauthorized, "Your session ended. Sign in and contact support with your payment ID.")
		return
	}
	if _, err := s.paymentControl(r.Context(), paymentControlRequest{Operation: "verify", Account: p.User.Email}); err != nil {
		paymentControlFailure(w, err)
		return
	}
	amount, ok := s.cartAmountPaise(r)
	if !ok || amount != state.Amount {
		paymentError(w, http.StatusBadRequest, "Your cart total changed during payment. Contact support with your payment ID.")
		return
	}
	result, err := s.checkoutCart(r, p.AccessToken, state.Address, float64(state.Amount)/100, &backend.PaymentProof{
		PaymentID: in.PaymentID,
		OrderID:   in.OrderID,
		Signature: in.Signature,
	})
	if err != nil {
		status := http.StatusInternalServerError
		var apiErr *backend.APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
			status = apiErr.Status
		}
		paymentError(w, status, "Payment was verified, but the order could not be placed. Contact support with payment ID "+in.PaymentID+".")
		return
	}

	shop.SaveCart(w, nil)
	s.clearPaymentState(w, r)
	ids := make([]string, 0, len(result.Orders))
	for _, order := range result.Orders {
		ids = append(ids, order.ID)
	}
	paymentJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"redirect": "/orders/placed?ids=" + url.QueryEscape(strings.Join(ids, ",")),
	})
}

func (s *Site) cartAmountPaise(r *http.Request) (int64, bool) {
	entries := s.resolveCartEntries(r.Context(), shop.ReadCart(r))
	if len(entries) == 0 {
		return 0, false
	}
	total := shop.CartSubtotal(entries) + shop.ChargesTotal(s.charges(r.Context()))
	return int64(math.Round(total * 100)), true
}

func (s *Site) checkoutCart(r *http.Request, token, addressID string, expected float64, payment *backend.PaymentProof) (backend.CheckoutResult, error) {
	entries := s.resolveCartEntries(r.Context(), shop.ReadCart(r))
	if len(entries) == 0 {
		return backend.CheckoutResult{}, errors.New("cart is empty")
	}
	lines := make([]backend.CheckoutLine, 0, len(entries))
	for _, entry := range entries {
		id, picked := shop.SplitLine(entry.Product.ID)
		lines = append(lines, backend.CheckoutLine{ItemID: id, Units: entry.Qty, Options: picked})
	}
	return s.backend.Checkout(r.Context(), token, lines, addressID, shop.CheckoutRequestID(r), expected, payment)
}

func verifyPaymentSignature(orderID, paymentID, signature, secret string) bool {
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(orderID + "|" + paymentID))
	return hmac.Equal(mac.Sum(nil), provided)
}

func (s *Site) setPaymentState(w http.ResponseWriter, r *http.Request, state paymentState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(s.razorpayKeySecret))
	_, _ = mac.Write([]byte(encoded))
	value := encoded + "." + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name: razorpayStateCookie, Value: value, Path: "/", MaxAge: int(razorpayStateTTL.Seconds()),
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Site) readPaymentState(r *http.Request) (paymentState, error) {
	var state paymentState
	cookie, err := r.Cookie(razorpayStateCookie)
	if err != nil {
		return state, err
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return state, errors.New("invalid payment state")
	}
	provided, err := hex.DecodeString(parts[1])
	if err != nil {
		return state, err
	}
	mac := hmac.New(sha256.New, []byte(s.razorpayKeySecret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(mac.Sum(nil), provided) {
		return state, errors.New("invalid payment state signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return state, err
	}
	return state, nil
}

func (s *Site) clearPaymentState(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: razorpayStateCookie, Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func integerValue(v any) (int64, bool) {
	switch value := v.(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		if value == math.Trunc(value) {
			return int64(value), true
		}
	}
	return 0, false
}

func decodePaymentJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func paymentJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func paymentError(w http.ResponseWriter, status int, message string) {
	paymentJSON(w, status, map[string]string{"error": message})
}

// Re-read authoritative availability before creating a provider order. Cached
// discovery cards may be a few seconds old; a charge cannot rely on them.
func (s *Site) checkNewPaymentAvailability(r *http.Request) error {
	ctx := backend.Fresh(r.Context())
	var availability struct {
		DeliveryAvailable bool `json:"deliveryAvailable"`
	}
	if err := s.backend.Get(ctx, "/api/availability", "", &availability); err != nil {
		return errors.New("Could not check delivery availability. Please try again.")
	}
	if !availability.DeliveryAvailable {
		return errors.New("Delivery is unavailable while all riders are off. Please try again when a rider is online.")
	}
	for _, line := range shop.ReadCart(r) {
		product, err := s.cartProduct(ctx, line.ID)
		if err != nil {
			return errors.New("An item is no longer available. Refresh your cart before paying.")
		}
		if product.StoreClosed {
			return errors.New(product.Store + " is closed today. Come back tomorrow or choose another store.")
		}
	}
	return nil
}
