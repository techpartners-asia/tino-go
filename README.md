# Tino Go Client

A Go client library for integrating with the [Tino](https://tino.mn) payment gateway API.

## Requirements

- Go 1.27 or later (see `go.mod`)

## Installation

```bash
go get github.com/techpartners-asia/tino-go
```

## Quick start

```go
package main

import (
	"log"

	tino "github.com/techpartners-asia/tino-go"
)

func main() {
	client := tino.New(
		"https://auth-sandbox.tino.mn/api/v1",    // authUrl
		"https://payment-sandbox.tino.mn/api/v1", // baseUrl
		"merchant-username",
		"merchant-password",
	)

	invoice, err := client.CreateInvoice(&tino.InvoiceRequest{
		WalletID:      "your-wallet-id",
		Amount:        15000,
		Description:   "Order #1234",
		TransactionID: "your-internal-id",
		CallbackURL:   "https://your-app.example/tino/callback",
		ExpiresMinute: 30,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println(invoice.Data.InvoiceID, invoice.Data.QrCode, invoice.Data.DeepLink)
}
```

**The caller owns the merchant token.** The client does not cache one, does not
renew it in the background, and does not log in on your behalf: call `Login`,
install the result with `SetToken`, and every call carries it. `New()` performs
no network I/O, and the token accessors are mutex-guarded, so a client is safe
for concurrent use.

```go
client := tino.New(authURL, baseURL, username, password)

token, err := client.Login(ctx)
if err != nil {
	return err
}
client.SetToken(token)
```

`Token.ExpiresAt` is when to replace it, and is always set: when the gateway
omits `expires_at`, `Login` fills in `FallbackTokenTTL` rather than leaving a
zero time that would make every call re-authenticate. Tino exposes no refresh
endpoint, so renewal is another `Login`.

Two errors are worth matching with `errors.Is`:

- `ErrNoToken` — a call was made before `SetToken`. A wiring mistake, not a
  gateway failure.
- `ErrUnauthorized` — the gateway refused the token with `401`/`403`. Log in
  again and retry; see below.

`WithToken(t)` installs a token at construction, for a caller that already holds
a valid one.

## API

| Method | Description |
| --- | --- |
| `CreateInvoice(*InvoiceRequest) (*InvoiceResponse, error)` | Нэхэмжлэх үүсгэх — create an invoice |
| `CheckInvoice(invoiceID string) (*InvoiceCheckResponse, error)` | Нэхэмжлэхийн төлөв шалгах — check invoice status |
| `CancelInvoice(invoiceID string) (bool, error)` | Нэхэмжлэх цуцлах — cancel an invoice |
| `GetUser(token string) (*UserInfoResponse, error)` | Хэрэглэгчийн мэдээлэл авах — resolve a mini-app user token |
| `SendNotification(*NotificationRequest) (*NotificationResponse, error)` | Mini-app хэрэглэгчид push notification илгээх |
| `CheckAutoSettlementOutbox(invoiceID string) (*AutoSettlementOutboxResponse, error)` | Авто тооцооны outbox invoice шалгах |

### Push notifications

`SendNotification` uses its own Basic Auth credentials, separate from the
merchant login passed to `New`. They are sent as an `Authorization` header only
and are never serialised into the request body.

```go
_, err := client.SendNotification(&tino.NotificationRequest{
	Auth:   &tino.BasicAuth{Username: "notif-user", Password: "notif-pass"},
	App:    "zahii", // mini-app slug, becomes {baseUrl}/{app}/notification
	UserID: "user-id",
	Title:  "Төлбөр амжилттай",
	Body:   "Таны төлбөр баталгаажлаа.",
})
```

### Payment callbacks

Decode the gateway's callback POST into `InvoiceCallbackResponse`:

```go
var cb tino.InvoiceCallbackResponse
if err := json.NewDecoder(r.Body).Decode(&cb); err != nil {
	// ...
}
// cb.Data.InvoiceID, cb.Data.Status, cb.Data.Amount
```

Always confirm a callback with `CheckInvoice` before releasing goods — treat the
callback as a signal, not as proof of payment.

## Error handling

Every method returns a non-nil `error` whenever the call did not succeed, which
includes all of the following:

- a transport failure, or an HTTP status outside `2xx`
- a response body that is empty or is not valid JSON
- a `2xx` response carrying `{"status": false}` — the gateway reports business
  failures such as *insufficient funds* this way

**On error the result pointer is `nil`.** Always check `err` before
dereferencing:

```go
res, err := client.CreateInvoice(req)
if err != nil {
	return err // res is nil here
}
use(res.Data.InvoiceID)
```

If the gateway rejects the installed token with `401`/`403`, the call returns
`ErrUnauthorized`. The client no longer re-authenticates and retries by itself —
it cannot, because replacing a token it does not own would be invisible to
whoever does. The retry is two lines, and it is safe for every verb here: a
refused request was never processed, so nothing can be double-created.

```go
res, err := client.CreateInvoice(req)
if errors.Is(err, tino.ErrUnauthorized) {
	token, err := client.Login(ctx)
	if err != nil {
		return err
	}
	client.SetToken(token)
	res, err = client.CreateInvoice(req)
}
```

## Custom HTTP client

`WithClient` replaces the default client entirely, so set your own timeout and
response body limit:

```go
rc := resty.New().
	SetTimeout(30 * time.Second).
	SetResponseBodyLimit(10 << 20)

client := tino.New(authURL, baseURL, user, pass, tino.WithClient(rc))
```

## Testing

```bash
go test -race ./...
```

`regression_test.go` runs against an in-process mock gateway and needs no
network access or credentials. `tests/get-user_test.go` calls the live sandbox
and requires valid sandbox credentials.

## License

MIT — see [LICENSE](LICENSE).
