package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"instalbloc/internal/config"
)

// NetopiaCheckoutSession describes an integer-bani payment checkout session.
type NetopiaCheckoutSession struct {
	InvoiceID      int64  `json:"invoice_id"`
	UserID         int64  `json:"user_id"`
	AmountBani     int64  `json:"amount_bani"`
	AmountRON      string `json:"amount_ron"`
	Currency       string `json:"currency"`
	OrderID        string `json:"order_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Signature      string `json:"signature"`
	CheckoutURL    string `json:"checkout_url"`
}

// NetopiaService handles payment checkout generation and constant-time HMAC-SHA256 IPN webhook verification.
type NetopiaService struct {
	cfg *config.Config
}

func NewNetopiaService(cfg *config.Config) *NetopiaService {
	return &NetopiaService{cfg: cfg}
}

// BuildCheckoutSession constructs a signed Netopia checkout session using strict int64 bani subdivisions.
func (s *NetopiaService) BuildCheckoutSession(invoiceID, userID, amountBani int64, invoiceNumber string) (*NetopiaCheckoutSession, error) {
	if invoiceID <= 0 || userID <= 0 {
		return nil, errors.New("netopia: ID factură sau utilizator invalid")
	}
	if amountBani <= 0 {
		return nil, errors.New("netopia: suma de plată (bani) trebuie să fie strict pozitivă")
	}

	whole := amountBani / 100
	rem := amountBani % 100
	amountRON := fmt.Sprintf("%d.%02d", whole, rem)

	nowUnix := time.Now().Unix()
	orderID := fmt.Sprintf("NTP-ORD-%d-%d", invoiceID, nowUnix)
	idempotencyKey := fmt.Sprintf("NTP-IDEMP-%d-%s", invoiceID, strings.TrimSpace(invoiceNumber))

	canonical := fmt.Sprintf("%d|%d|%d|RON|%s", invoiceID, userID, amountBani, idempotencyKey)
	sig := s.SignPayload([]byte(canonical))

	appURL := strings.TrimRight(strings.TrimSpace(s.cfg.AppURL), "/")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}
	q := url.Values{}
	q.Set("invoice_id", strconv.FormatInt(invoiceID, 10))
	q.Set("order_id", orderID)
	q.Set("amount_bani", strconv.FormatInt(amountBani, 10))
	q.Set("sig", sig)

	return &NetopiaCheckoutSession{
		InvoiceID:      invoiceID,
		UserID:         userID,
		AmountBani:     amountBani,
		AmountRON:      amountRON,
		Currency:       "RON",
		OrderID:        orderID,
		IdempotencyKey: idempotencyKey,
		Signature:      sig,
		CheckoutURL:    fmt.Sprintf("%s/billing?checkout=%s", appURL, url.QueryEscape(orderID)),
	}, nil
}

// SignPayload computes the HMAC-SHA256 hex digest of a raw webhook or checkout payload.
func (s *NetopiaService) SignPayload(raw []byte) string {
	secret := s.cfg.HMACSecret
	if secret == "" {
		secret = "default-dev-hmac-secret-min-16-chars"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature validates the X-Netopia-Signature header in constant time using hmac.Equal.
func (s *NetopiaService) VerifyWebhookSignature(raw []byte, providedSignature string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(providedSignature))
	trimmed = strings.TrimPrefix(trimmed, "sha256=")
	if trimmed == "" {
		return false
	}
	expectedHex := s.SignPayload(raw)
	providedBytes, err := hex.DecodeString(trimmed)
	if err != nil {
		return false
	}
	expectedBytes, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	return hmac.Equal(providedBytes, expectedBytes)
}
