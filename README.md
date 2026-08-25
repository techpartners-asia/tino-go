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

The client authenticates on first use and caches the merchant token, refreshing
it automatically before it expires. It is safe for concurrent use by multiple
goroutines.

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

If the gateway rejects the cached token with `401`/`403`, the client
re-authenticates and retries the call once, transparently.

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
