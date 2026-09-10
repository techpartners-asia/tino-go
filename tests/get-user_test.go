package tests

import (
	"context"
	"os"
	"testing"

	"github.com/techpartners-asia/tino-go"
)

// TestGetUser talks to the live Tino sandbox, so it is skipped unless
// TINO_LIVE_TEST is set. It also shows the token flow a caller now owns:
// Login, SetToken, then call.
func TestGetUser(t *testing.T) {
	if os.Getenv("TINO_LIVE_TEST") == "" {
		t.Skip("set TINO_LIVE_TEST=1 to run against the Tino sandbox")
	}

	client := tino.New(
		"https://auth-sandbox.tino.mn/api/v1",
		"https://payment-sandbox.tino.mn/api/v1",
		"test", "test",
	)

	token, err := client.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	client.SetToken(token)

	user, err := client.GetUser("test")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	t.Log(user)
}
