package tino

import "time"

type BaseResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
}

// login structs
type (
	AuthRequest struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	AuthResponse struct {
		BaseResponse
		Data AuthData `json:"data"`
	}
	AuthData struct {
		User            AuthUserData `json:"user"`
		Token           string       `json:"token"`
		RefreshToken    string       `json:"refresh_token"`
		RefreshExpireAt time.Time    `json:"refresh_expires_at"`
		ExpiresAt       time.Time    `json:"expires_at"`
	}
	AuthUserData struct {
		ID        string    `json:"id"`
		Email     string    `json:"email"`
		Profile   string    `json:"profile"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
)

// invoice structs
type (
	InvoiceRequest struct {
		WalletID      string `json:"wallet_id"` // Төлбөр хүлээн авах мерчантын хэтэвч
		ExpiresMinute int    `json:"expires_minute"`
		Description   string `json:"description"`
		Amount        int    `json:"amount"`
		TransactionID string `json:"transaction_id"` // third party id
		CallbackURL   string `json:"callback_url"`
		MetaData      string `json:"meta_data"`
	}
	InvoiceResponse struct {
		BaseResponse
		Data InvoiceData `json:"data"`
	}
	InvoiceData struct {
		InvoiceID   string    `json:"invoice_id"`
		MerchantID  string    `json:"merchant_id"`
		WalletID    string    `json:"wallet_id"`
		Amount      int       `json:"amount"`
		Status      string    `json:"status"`
		QrCode      string    `json:"qr_code"`
		ExpiresAt   time.Time `json:"expires_at"`
		CallbackURL string    `json:"callback_url"`
		CreatedAt   time.Time `json:"created_at"`
	}

	InvoiceCheckResponse struct {
		BaseResponse
		Data InvoiceCheckData `json:"data"`
	}
	InvoiceCheckData struct {
		InvoiceID      string    `json:"invoice_id"`
		Status         string    `json:"status"`
		Amount         int       `json:"amount"`
		CouponAmount   int       `json:"coupon_amount"`
		OriginalAmount int       `json:"original_amount"`
		PaidAt         time.Time `json:"paid_at"`
	}
	InvoiceCallbackResponse struct {
		BaseResponse
		Data InvoiceCallbackData `json:"data"`
	}
	InvoiceCallbackData struct {
		InvoiceID      string `json:"invoice_id"`
		Status         string `json:"status"`
		Amount         int    `json:"amount"`
		CouponAmount   int    `json:"coupon_amount"`
		OriginalAmount int    `json:"original_amount"`
	}
)
