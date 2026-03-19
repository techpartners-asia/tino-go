package tests

import (
	"fmt"
	"testing"

	"github.com/techpartners-asia/tino-go"
)

func TestGetUser(t *testing.T) {
	tino := tino.New("https://auth-sandbox.tino.mn/api/v1", "https://payment-sandbox.tino.mn/api/v1", "test", "test")
	user, err := tino.GetUser("test")
	if err != nil {
		t.Errorf("Error getting user: %v", err)
	}
	t.Log(user)
	fmt.Println(user)
}
