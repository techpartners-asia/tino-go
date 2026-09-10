package tino

import (
	"errors"
	"time"
)

// Token is a Tino merchant token together with everything the caller needs to
// decide when to replace it.
//
// The SDK does not cache tokens: [Tino.Login] performs exactly one HTTP call
// and returns what the gateway said, and [Tino.SetToken] installs the token
// the next request will carry. Whoever owns the client owns the token's
// lifetime — expiry tracking, renewal scheduling and deduplication of
// concurrent logins are all the caller's, because only the caller knows
// whether the token is shared beyond this process.
type Token struct {
	// AccessToken is the bearer sent as Authorization on every API call.
	AccessToken string

	// RefreshToken is returned by the gateway. Tino exposes no refresh
	// endpoint, so it is carried for completeness only: renew with
	// [Tino.Login].
	RefreshToken string

	// ExpiresAt is when AccessToken stops being accepted. Tino sometimes omits
	// expires_at, in which case [Tino.Login] fills in [FallbackTokenTTL] from
	// now rather than leaving a zero time that would make every call
	// re-authenticate.
	ExpiresAt time.Time

	// RefreshExpiresAt is the gateway's refresh_expires_at, verbatim.
	RefreshExpiresAt time.Time

	// User is the merchant account the token was issued for.
	User AuthUserData
}

// IsZero reports whether the token carries no credential at all.
func (t Token) IsZero() bool { return t.AccessToken == "" }

var (
	// ErrNoToken is returned when an API call is attempted before a token has
	// been installed with [Tino.SetToken]. The SDK never authenticates on its
	// own, so this is a wiring mistake in the caller, not a gateway failure.
	ErrNoToken = errors.New("tino: no access token installed: call Login then SetToken")

	// ErrUnauthorized is returned when Tino rejects the installed token with
	// 401 or 403 — revoked, or expired earlier than advertised. The caller
	// should discard the token, log in again and retry: the rejected request
	// was never processed by the gateway, so retrying cannot double-create an
	// invoice.
	//
	// The SDK used to perform that retry itself, from its own cache. It cannot
	// any more: replacing a token it does not own would be invisible to
	// whoever does.
	ErrUnauthorized = errors.New("tino: access token rejected")
)

// tokenFrom converts a login response into a Token, applying the fallback TTL
// when the gateway omitted an expiry.
func tokenFrom(data AuthData) Token {
	expiresAt := data.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(FallbackTokenTTL)
	}

	return Token{
		AccessToken:      data.Token,
		RefreshToken:     data.RefreshToken,
		ExpiresAt:        expiresAt,
		RefreshExpiresAt: data.RefreshExpireAt,
		User:             data.User,
	}
}
