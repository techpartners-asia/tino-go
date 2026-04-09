package tino

import (
	"errors"
	"sync"
	"time"

	"resty.dev/v3"
)

type tino struct {
	authUrl  string
	baseUrl  string
	username string
	password string

	auth *AuthData
	mu   sync.RWMutex
	refreshMu sync.Mutex // Serializes re-auth calls when mu is unlocked

	client *resty.Client
}

// Tino [Tino SDK Interface / Интерфэйс]
type Tino interface {
	// CreateInvoice [Нэхэмжлэх үүсгэх]
	CreateInvoice(invoice *InvoiceRequest) (*InvoiceResponse, error)

	// CancelInvoice [Нэхэмжлэх цуцлах]
	CancelInvoice(invoiceId string) (bool, error)

	// CheckInvoice [Нэхэмжлэхийн төлөв шалгах]
	CheckInvoice(invoiceId string) (*InvoiceCheckResponse, error)

	// GetUser [Хэрэглэгчийн мэдээлэл авах]
	GetUser(token string) (*UserInfoResponse, error)
}

// Option defines an option for tino initialization.
type Option func(*tino)

// WithClient [Custom resty.Client ашиглах]
// This is useful for injecting a client with custom timeouts, certificates, etc.
func WithClient(client *resty.Client) Option {
	return func(t *tino) {
		if client != nil {
			t.client = client
		}
	}
}

// New [Tino SDK-ийг шинээр үүсгэх]
// authUrl: Нэвтрэлт болон хэрэглэгчийн мэдээлэл авах URL
// baseUrl: Төлбөрийн API-н үндсэн URL
// username: Мерчантын нэвтрэх нэр
// password: Мерчантын нууц үг
func New(authUrl, baseUrl, username, password string, options ...Option) Tino {
	t := &tino{
		authUrl:  authUrl,
		baseUrl:  baseUrl,
		username: username,
		password: password,
		client:   resty.New().SetTimeout(60 * time.Second),
	}

	for _, opt := range options {
		opt(t)
	}

	// Attempt login in background to warm the token cache.
	// If it fails (network down or bad config), authTino will retry
	// transparently on the first real API call.
	go t.authTino() //nolint:errcheck

	return t
}

// CreateInvoice [Нэхэмжлэх үүсгэх]
func (t *tino) CreateInvoice(invoice *InvoiceRequest) (*InvoiceResponse, error) {
	var response InvoiceResponse
	err := t.httpRequest(t.baseUrl, invoice, &response, TinoInvoiceCreate, "")
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// CancelInvoice [Нэхэмжлэх цуцлах]
func (t *tino) CancelInvoice(invoiceId string) (bool, error) {
	var response InvoiceResponse
	err := t.httpRequest(t.baseUrl, nil, &response, TinoInvoiceCancel, invoiceId+"?reason=canceled")
	if err != nil {
		return false, err
	}
	if response.Data.Status != "cancelled" {
		return false, errors.New("invoice not cancelled")
	}
	return true, nil
}

// CheckInvoice [Нэхэмжлэхийн төлөв шалгах]
func (t *tino) CheckInvoice(invoiceId string) (*InvoiceCheckResponse, error) {
	var response InvoiceCheckResponse
	err := t.httpRequest(t.baseUrl, nil, &response, TinoInvoiceCheck, invoiceId)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// GetUser [Хэрэглэгчийн мэдээлэл авах]
func (t *tino) GetUser(token string) (*UserInfoResponse, error) {
	var response UserResponse
	err := t.httpRequest(t.authUrl, nil, &response, TinoGetUser, token)
	if err != nil {
		return nil, err
	}
	if !response.Status {
		return nil, errors.New(response.Message)
	}
	return &response.Data, nil
}
