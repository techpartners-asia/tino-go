package tests

import (
	"fmt"
	"testing"

	"github.com/techpartners-asia/tino-go"
)

func TestGetUser(t *testing.T) {
	tino := tino.New("https://auth-sandbox.tino.mn/api/v1", "https://payment-sandbox.tino.mn/api/v1", "ZAHII", "lGDixnEKUPvGHnl")
	user, err := tino.GetUser("5545ff7dfc37c905061653a61c272f5f")
	if err != nil {
		t.Errorf("Error getting user: %v", err)
	}
	t.Log(user)
	fmt.Println(user)
}
