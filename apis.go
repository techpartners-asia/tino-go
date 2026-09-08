package tino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"resty.dev/v3"
)

// api defines an HTTP endpoint with its URL path and method.
type api struct {
	Url    string
	Method string
}

const (
	// maxErrorBodyLen caps how much of a response body is embedded into an error
	// so a large or hostile response cannot blow up the caller's logs.
	maxErrorBodyLen = 512

	// maxResponseBodySize bounds an individual response body.
	maxResponseBodySize = 10 << 20 // 10 MiB
)

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
	// TinoSendNotification [Mini-app хэрэглэгчид push notification илгээх]
	// Url suffix only — the app slug is prefixed per-call from NotificationRequest.App.
	TinoSendNotification = api{
		Url:    "/notification",
		Method: http.MethodPost,
	}
	// TinoAutoSettlementOutboxInvoice [Авто тооцооны outbox invoice шалгах]
	TinoAutoSettlementOutboxInvoice = api{
		Url:    "/merchant/settlements/auto-settlement-outbox/invoice/",
		Method: http.MethodGet,
	}
)

// buildURL [Internal: URL-ийг аюулгүйгээр угсрах]
//
// The trailing path segment is percent-escaped with [url.PathEscape]. Callers
// feed attacker-influenced values in here (mini-app tokens, invoice IDs), and
// raw concatenation let those values escape the endpoint: "../../x" walked to a
// different API, "a?b=c" injected query parameters, and "a#b" truncated the URL
// at the fragment so the request silently addressed a different resource.
func buildURL(baseURL string, endpoint api, segment string, query url.Values) string {
	u := baseURL + endpoint.Url
	if segment != "" {
		u += url.PathEscape(segment)
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// truncateBody trims a response body down to a log-safe length.
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrorBodyLen {
		s = strings.ToValidUTF8(s[:maxErrorBodyLen], "") + "…(truncated)"
	}
	return s
}

// messageOr returns msg, or fallback when msg is blank. The gateway often
// reports a logical failure with an empty `message`, and errors.New("") yields
// a non-nil error whose text is empty — impossible to diagnose from a log.
func messageOr(msg, fallback string) string {
	if strings.TrimSpace(msg) == "" {
		return fallback
	}
	return msg
}

// isNil reports whether v is nil, including a nil pointer boxed in an interface.
// `body any` holding a (*InvoiceRequest)(nil) is NOT == nil, so a plain
// `body != nil` check let a nil request through and serialised it as `null`.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// finish [Internal: Хариуг шалгаж, задлах]
//
// It always reads the body first. Resty only auto-closes the body for
// 2xx-with-result and >=400-with-error responses, so 1xx/3xx replies would
// otherwise hold the connection out of the pool forever.
//
// The status test is `!IsStatusSuccess()` rather than `IsStatusFailure()`:
// the latter is `code > 399`, which classifies a 1xx or a 3xx (say, a redirect
// from a proxy or WAF) as success and hands the caller a zero-valued struct
// with a nil error.
func finish(res *resty.Response, result any) error {
	if res == nil {
		return errors.New("tino: nil response")
	}

	body := res.Bytes() // reads the body and releases the connection
	if res.Body != nil {
		defer func() { _ = res.Body.Close() }()
	}

	if !res.IsStatusSuccess() {
		return fmt.Errorf(
			"%s-Tino response error: %s (Status: %d)",
			time.Now().Format("2006-01-02 15:04:05"),
			truncateBody(body),
			res.StatusCode(),
		)
	}

	if result == nil {
		return nil
	}

	// A 2xx with an empty or non-JSON body (an HTML error page from a proxy,
	// for instance) used to leave `result` zero-valued and return nil, so the
	// caller saw a successful call with an empty invoice.
	if len(body) == 0 {
		return fmt.Errorf("Tino response error: empty response body (Status: %d)", res.StatusCode())
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf(
			"Tino response error: cannot decode response (Status: %d, body: %s): %w",
			res.StatusCode(), truncateBody(body), err,
		)
	}

	return nil
}

// newRequest builds a request carrying the bearer token and JSON body.
func (t *tino) newRequest(token string, body any) *resty.Request {
	req := t.client.R().
		SetHeader("Content-Type", "application/json").
		SetResponseBodyLimit(maxResponseBodySize)

	if token != "" {
		req.SetAuthToken(token)
	}
	if !isNil(body) {
		req.SetBody(body)
	}
	return req
}

// httpRequestBasicAuth [Internal: Tino API-руу Basic Authentication ашиглан HTTP хүсэлт илгээх туслах функц]
// baseURL: Хүсэлт илгээх үндсэн URL
// body: Хүсэлтийн бие (POST/PUT үед)
// result: Хариуг задлах бүтэц (struct pointer)
// endpoint: API төрлийн эндпоинтын тохиргоо
// urlExt: URL-д залгагдах нэмэлт зам эсвэл ID (notification_id гэх мэт)
//
// Note:
//   - Basic Authentication-д config.yml дахь tino-client.username болон
//     tino-client.password тохиргоог ашиглахгүй (хэрэв notofication дээр ашиглахаар бол).
//   - Bearer Token ашигладаг httpRequest функцээс тусдаа ажиллана.
func (t *tino) httpRequestBasicAuth(baseURL string, body any, result any, endpoint api, urlExt string, basicAuth *BasicAuth) error {
	if basicAuth == nil {
		return errors.New("tino: basic auth credentials are required")
	}

	req := t.client.R().
		SetHeader("Content-Type", "application/json").
		SetResponseBodyLimit(maxResponseBodySize).
		SetBasicAuth(basicAuth.Username, basicAuth.Password)

	if !isNil(body) {
		req.SetBody(body)
	}

	res, err := req.Execute(endpoint.Method, buildURL(baseURL, endpoint, urlExt, nil))
	if err != nil {
		return err
	}

	return finish(res, result)
}

// httpRequest [Internal: Tino API-руу HTTP хүсэлт илгээх туслах функц]
// baseUrl: Хүсэлт илгээх үндсэн URL (authUrl эсвэл baseUrl)
// body: Хүсэлтийн бие (POST үед)
// result: Хариуг задлах бүтэц (struct pointer)
// endpoint: api төрлийн эндпоинт тохиргоо
// urlExt: URL-д залгагдах нэмэлт ID (invoice_id г.м) — дотооддоо escape хийгдэнэ
// query: Query parameter-ууд (заавал биш)
func (t *tino) httpRequest(baseUrl string, body any, result any, endpoint api, urlExt string, query url.Values) error {
	token := t.Token()
	if token.IsZero() {
		// The SDK no longer authenticates behind the caller's back, so an
		// absent token is reported rather than quietly fetched.
		return ErrNoToken
	}

	target := buildURL(baseUrl, endpoint, urlExt, query)

	res, err := t.newRequest(token.AccessToken, body).Execute(endpoint.Method, target)
	if err != nil {
		return err
	}

	// A 401/403 means the token was rejected — revoked, or expired earlier
	// than advertised. The SDK used to drop it and retry from its own cache;
	// with the token owned outside, it reports ErrUnauthorized instead so the
	// owner can replace it and retry. Retrying is safe for every verb here: a
	// rejected request was never processed by the gateway, so no invoice can
	// be double-created.
	if res.StatusCode() == http.StatusUnauthorized || res.StatusCode() == http.StatusForbidden {
		body := res.Bytes()
		if res.Body != nil {
			_ = res.Body.Close()
		}
		return fmt.Errorf("%w (Status: %d): %s", ErrUnauthorized,
			res.StatusCode(), truncateBody(body))
	}

	return finish(res, result)
}

// FallbackTokenTTL is the lifetime [Tino.Login] assumes when the gateway
// omits expires_at. Without it a zero expiry makes every single call
// re-authenticate, which turns normal traffic into a login flood.
const FallbackTokenTTL = 5 * time.Minute

// Login [Tino-гоос Access Token авах]
//
// Login performs exactly one request and caches nothing: the returned token is
// the caller's to hold, store and install with [Tino.SetToken]. Concurrent
// callers each issue their own request, so deduplicating them is the caller's
// job too — the SDK has no way to know whether a token is shared across
// processes.
func (t *tino) Login(ctx context.Context) (Token, error) {
	var response AuthResponse
	res, err := t.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetResponseBodyLimit(maxResponseBodySize).
		SetBody(AuthRequest{
			Username: t.username,
			Password: t.password,
		}).
		Post(t.authUrl + TinoMerchantLogin.Url)

	if err != nil {
		return Token{}, err
	}

	if err := finish(res, &response); err != nil {
		return Token{}, fmt.Errorf("%s-Tino auth failed: %w",
			time.Now().Format("2006-01-02 15:04:05"), err)
	}

	// The gateway answers HTTP 200 with {"status":false} for bad credentials.
	// Only the HTTP status used to be checked, so an empty token was returned
	// as a success and every later call went out with no Authorization header
	// at all.
	if !response.Status {
		return Token{}, fmt.Errorf("tino auth failed: %s",
			messageOr(response.Message, "authentication rejected by gateway"))
	}
	if response.Data.Token == "" {
		return Token{}, errors.New("tino auth failed: response contained no token")
	}

	return tokenFrom(response.Data), nil
}

// SetToken installs the token subsequent calls will carry. Passing the zero
// Token clears it, which makes the next call fail with [ErrNoToken] rather
// than reach Tino unauthenticated.
func (t *tino) SetToken(token Token) {
	t.mu.Lock()
	t.token = token
	t.mu.Unlock()
}

// Token returns the installed token.
func (t *tino) Token() Token {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.token
}
