package tino

import (
	"errors"
	"time"

	"resty.dev/v3"
)

type tino struct {
	authUrl  string
	baserUrl string
	username string
	password string
	client   *AuthData
}

type Tino interface {
	CreateInvoice(invoice *InvoiceRequest) (*InvoiceResponse, error)
	CancelInvoice(invoiceId string) (bool, error)
	CheckInvoice(invoiceId string) (*InvoiceCheckResponse, error)
	CheckTokenExpire() error
}

func New(authUrl string, baseURL string, username string, password string) Tino {
	return &tino{
		authUrl:  authUrl,
		baserUrl: baseURL,
		username: username,
		password: password,
	}
}

func (t *tino) CreateInvoice(invoice *InvoiceRequest) (*InvoiceResponse, error) {
	err := t.CheckTokenExpire()
	if err != nil {
		return nil, err
	}
	client := resty.New()
	defer client.Close()
	var response *InvoiceResponse
	res, err := client.R().
		SetAuthToken(t.client.Token.Token).
		SetBody(invoice).              // default request content type is JSON
		SetResult(&InvoiceResponse{}). // or SetResult(LoginResponse{}).
		SetError(&InvoiceResponse{}).  // or SetError(LoginError{}).
		Post(t.baserUrl + "/merchant/invoice")
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, errors.New(res.Error().(string))
	}
	response = res.Result().(*InvoiceResponse)
	return response, nil
}

func (t *tino) CancelInvoice(invoiceId string) (bool, error) {
	err := t.CheckTokenExpire()
	if err != nil {
		return false, err
	}
	client := resty.New()
	defer client.Close()
	var response *InvoiceResponse
	res, err := client.R().
		SetAuthToken(t.client.Token.Token).
		SetQueryParam("reason", "canceled").
		SetResult(&InvoiceResponse{}). // or SetResult(LoginResponse{}).
		SetError(&InvoiceResponse{}).  // or SetError(LoginError{}).
		Post(t.baserUrl + "/merchant/invoice/cancel/" + invoiceId)
	if err != nil {
		return false, err
	}
	if res.IsError() {
		return false, errors.New(res.Error().(string))
	}
	response = res.Result().(*InvoiceResponse)
	if response.Data.Status != "cancelled" {
		return false, errors.New("invoice not cancelled")
	}
	return true, nil
}

func (t *tino) CheckInvoice(invoiceId string) (*InvoiceCheckResponse, error) {
	err := t.CheckTokenExpire()
	if err != nil {
		return nil, err
	}
	client := resty.New()
	defer client.Close()
	var response *InvoiceCheckResponse
	res, err := client.R().
		SetAuthToken(t.client.Token.Token).
		SetResult(&InvoiceCheckResponse{}). // or SetResult(LoginResponse{}).
		SetError(&InvoiceCheckResponse{}).  // or SetError(LoginError{}).
		Get(t.baserUrl + "/merchant/invoice/" + invoiceId)
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, errors.New(res.Error().(string))
	}
	response = res.Result().(*InvoiceCheckResponse)
	return response, nil
}

func (t *tino) CheckTokenExpire() error {
	if !t.client.Token.ExpiresAt.Before(time.Now()) {
		client := resty.New()
		defer client.Close()
		res, err := client.R().
			SetBody(AuthRequest{
				Username: t.username,
				Password: t.password,
			}).
			SetResult(&AuthResponse{}). // or SetResult(LoginResponse{}).
			SetError(&AuthResponse{}).  // or SetError(LoginError{}).
			Post(t.authUrl + "/merchant/login")
		if err != nil {
			return err
		}
		if res.IsError() {
			return errors.New(res.Error().(string))
		}
		response := res.Result().(*AuthResponse)
		t.client = &response.Data
	}
	return nil
}
