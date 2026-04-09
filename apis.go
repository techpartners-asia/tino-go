package tino

import (
	"fmt"
	"net/http"
	"time"
)

// api defines an HTTP endpoint with its URL path and method.
type api struct {
	Url    string
	Method string
}

var (
	// TinoMerchantLogin [Мерчант нэвтрэх]
	TinoMerchantLogin = api{
		Url:    "/merchant/login",
		Method: http.MethodPost,
	}
	// TinoInvoiceCreate [Нэхэмжлэх үүсгэх]
	TinoInvoiceCreate = api{
		Url:    "/merchant/invoice",
		Method: http.MethodPost,
	}
	// TinoInvoiceCancel [Нэхэмжлэх цуцлах]
	TinoInvoiceCancel = api{
		Url:    "/merchant/invoice/cancel/",
		Method: http.MethodPost,
	}
	// TinoInvoiceCheck [Нэхэмжлэхийн төлөв шалгах]
	TinoInvoiceCheck = api{
		Url:    "/merchant/invoice/",
		Method: http.MethodGet,
	}
	// TinoGetUser [Хэрэглэгчийн мэдээлэл авах]
	TinoGetUser = api{
		Url:    "/auth/miniapp/",
		Method: http.MethodGet,
	}
)

// httpRequest [Internal: Tino API-руу HTTP хүсэлт илгээх туслах функц]
// baseUrl: Хүсэлт илгээх үндсэн URL (authUrl эсвэл baseUrl)
// body: Хүсэлтийн бие (POST үед)
// result: Хариуг задлах бүтэц (struct pointer)
// endpoint: api төрлийн эндпоинт тохиргоо
// urlExt: URL-д залгагдах нэмэлт ID (invoice_id г.м)
func (t *tino) httpRequest(baseUrl string, body any, result any, endpoint api, urlExt string) error {
	_, authErr := t.authTino()
	if authErr != nil {
		return authErr
	}

	// Ensure thread safety for token fetch
	t.mu.RLock()
	token := ""
	if t.auth != nil {
		token = t.auth.Token
	}
	t.mu.RUnlock()

	url := baseUrl + endpoint.Url + urlExt
	req := t.client.R().
		SetHeader("Content-Type", "application/json").
		SetAuthToken(token).
		SetResult(result)

	if body != nil {
		req.SetBody(body)
	}

	res, err := req.Execute(endpoint.Method, url)
	if err != nil {
		return err
	}

	if res.IsError() {
		return fmt.Errorf("%s-Tino response error: %s (Status: %d)",
			time.Now().Format("2006-01-02 15:04:05"),
			res.String(),
			res.StatusCode())
	}

	return nil
}

// authTino [Internal: Tino-гоос Access Token авах/шинэчлэх]
// Энэ функц нь токен дуусах хугацааг шалгаж, шаардлагатай бол автоматаар шинэчилнэ.
func (t *tino) authTino() (authRes AuthData, err error) {
	// 1. Fast path: Read-lock check
	t.mu.RLock()
	if t.auth != nil && t.auth.ExpiresAt.After(time.Now().Add(1*time.Minute)) {
		authRes = *t.auth
		t.mu.RUnlock()
		return authRes, nil
	}
	t.mu.RUnlock()

	// 2. Slow path: Acquire refresh lock (serializes the network call)
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()

	// 3. Double-check token state with Read-lock after acquiring refreshMu
	// (Another goroutine might have refreshed it while we were waiting on refreshMu)
	t.mu.RLock()
	if t.auth != nil && t.auth.ExpiresAt.After(time.Now().Add(1*time.Minute)) {
		authRes = *t.auth
		t.mu.RUnlock()
		return authRes, nil
	}
	t.mu.RUnlock()

	// 4. Perform the actual network refresh (outside the main 'mu' to keep it responsive)
	var response AuthResponse
	url := t.authUrl + TinoMerchantLogin.Url
	res, err := t.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(AuthRequest{
			Username: t.username,
			Password: t.password,
		}).
		SetResult(&response).
		Post(url)

	if err != nil {
		return authRes, err
	}

	if res.IsError() {
		return authRes, fmt.Errorf("%s-Tino auth failed: %s (Status: %d)",
			time.Now().Format("2006-01-02 15:04:05"),
			res.String(),
			res.StatusCode())
	}

	// 5. Update shared state under Write-lock
	t.mu.Lock()
	t.auth = &response.Data
	t.mu.Unlock()

	return response.Data, nil
}
