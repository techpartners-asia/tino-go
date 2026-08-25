package tino

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"resty.dev/v3"
)

type tino struct {
	authUrl  string
	baseUrl  string
	username string
	password string

	auth      *AuthData
	mu        sync.RWMutex
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

	// SendNotification [Mini-app хэрэглэгчид push notification илгээх]
	SendNotification(req *NotificationRequest) (*NotificationResponse, error)

	// CheckAutoSettlementOutbox [Авто тооцооны outbox invoice шалгах]
	CheckAutoSettlementOutbox(invoiceId string) (*AutoSettlementOutboxResponse, error)
}

// Option defines an option for tino initialization.
type Option func(*tino)

// WithClient [Custom resty.Client ашиглах]
// This is useful for injecting a client with custom timeouts, certificates, etc.
//
// A custom client replaces the default one entirely, so it should set its own
// timeout and response body limit.
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
		authUrl:  strings.TrimSuffix(authUrl, "/"),
		baseUrl:  strings.TrimSuffix(baseUrl, "/"),
		username: username,
		password: password,
		client: resty.New().
			SetTimeout(60 * time.Second).
			SetResponseBodyLimit(maxResponseBodySize),
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
	if invoice == nil {
		return nil, errors.New("tino: invoice request is required")
	}

	var response InvoiceResponse
	if err := t.httpRequest(t.baseUrl, invoice, &response, TinoInvoiceCreate, "", nil); err != nil {
		return nil, err
	}

	// The gateway reports business failures as HTTP 200 + {"status":false}.
	// Without this check the caller received a nil error and an invoice with an
	// empty InvoiceID and a zero Amount.
	if !response.Status {
		return nil, fmt.Errorf("tino: create invoice failed: %s",
			messageOr(response.Message, "gateway rejected the invoice"))
	}

	return &response, nil
}

// CancelInvoice [Нэхэмжлэх цуцлах]
func (t *tino) CancelInvoice(invoiceId string) (bool, error) {
	if strings.TrimSpace(invoiceId) == "" {
		return false, errors.New("tino: invoice id is required")
	}

	var response InvoiceResponse
	// `reason` goes through url.Values instead of being concatenated onto the
	// ID: an ID containing '#' used to swallow the whole query string, and one
	// containing '?' produced a malformed duplicate parameter.
	query := url.Values{"reason": []string{"canceled"}}

	if err := t.httpRequest(t.baseUrl, nil, &response, TinoInvoiceCancel, invoiceId, query); err != nil {
		return false, err
	}
	if !response.Status {
		return false, fmt.Errorf("tino: cancel invoice failed: %s",
			messageOr(response.Message, "gateway rejected the cancellation"))
	}
	if response.Data.Status != "cancelled" {
		return false, fmt.Errorf("tino: invoice not cancelled (status: %q)", response.Data.Status)
	}

	return true, nil
}

// CheckInvoice [Нэхэмжлэхийн төлөв шалгах]
func (t *tino) CheckInvoice(invoiceId string) (*InvoiceCheckResponse, error) {
	if strings.TrimSpace(invoiceId) == "" {
		return nil, errors.New("tino: invoice id is required")
	}

	var response InvoiceCheckResponse
	if err := t.httpRequest(t.baseUrl, nil, &response, TinoInvoiceCheck, invoiceId, nil); err != nil {
		return nil, err
	}
	if !response.Status {
		return nil, fmt.Errorf("tino: check invoice failed: %s",
			messageOr(response.Message, "gateway returned an unsuccessful status"))
	}

	return &response, nil
}

// GetUser [Хэрэглэгчийн мэдээлэл авах]
func (t *tino) GetUser(token string) (*UserInfoResponse, error) {
	// An empty token used to produce a request to the bare collection endpoint
	// "/auth/miniapp/" rather than a lookup for one user.
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("tino: user token is required")
	}

	var response UserResponse
	if err := t.httpRequest(t.authUrl, nil, &response, TinoGetUser, token, nil); err != nil {
		return nil, err
	}
	if !response.Status {
		return nil, fmt.Errorf("tino: get user failed: %s",
			messageOr(response.Message, "gateway returned an unsuccessful status"))
	}

	return &response.Data, nil
}

// CheckAutoSettlementOutbox [Авто тооцооны outbox invoice шалгах]
func (t *tino) CheckAutoSettlementOutbox(invoiceId string) (*AutoSettlementOutboxResponse, error) {
	if strings.TrimSpace(invoiceId) == "" {
		return nil, errors.New("tino: invoice id is required")
	}

	var response AutoSettlementOutboxResponse
	if err := t.httpRequest(t.baseUrl, nil, &response, TinoAutoSettlementOutboxInvoice, invoiceId, nil); err != nil {
		return nil, err
	}
	if !response.Status {
		return nil, fmt.Errorf("tino: check auto-settlement outbox failed: %s",
			messageOr(response.Message, "gateway returned an unsuccessful status"))
	}

	return &response, nil
}

// SendNotification [Mini-app хэрэглэгчид push notification илгээх]
// Final URL: {baseUrl}/{req.App}/notification
func (t *tino) SendNotification(req *NotificationRequest) (*NotificationResponse, error) {
	if req == nil {
		return nil, errors.New("notification request is required")
	}
	if req.Auth == nil {
		return nil, errors.New("auth request is required")
	}
	if req.Auth.Username == "" {
		return nil, errors.New("auth username request is required")
	}
	if req.Auth.Password == "" {
		return nil, errors.New("auth password request is required")
	}
	if req.App == "" {
		return nil, errors.New("app is required")
	}
	if req.UserID == "" {
		return nil, errors.New("user_id is required")
	}

	// Prefix the app slug onto the endpoint path. Copy TinoSendNotification so
	// the shared api{} global stays immutable across concurrent callers, and
	// escape the slug so it cannot traverse to another endpoint.
	endpoint := api{
		Url:    "/" + url.PathEscape(req.App) + TinoSendNotification.Url,
		Method: TinoSendNotification.Method,
	}

	var response NotificationResponse
	if err := t.httpRequestBasicAuth(t.baseUrl, req, &response, endpoint, "", req.Auth); err != nil {
		return nil, err
	}
	if !response.Status {
		return nil, fmt.Errorf("tino: send notification failed: %s",
			messageOr(response.Message, "gateway returned an unsuccessful status"))
	}

	return &response, nil
}
