package tino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func okAuth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": true,
		"data": map[string]any{
			"token":      "TOK",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	})
}

type capture struct {
	uri, body, authz atomic.Value
}

func (c *capture) get(v *atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}

func newServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *capture) {
	c := &capture{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/merchant/login" {
			okAuth(w)
			return
		}
		c.uri.Store(r.URL.RequestURI())
		c.authz.Store(r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		c.body.Store(string(b))
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s, c
}

// newAuthed returns a client that has logged in and installed the token,
// which is the state every API-level test needs: the SDK no longer logs in on
// its own.
func newAuthed(t *testing.T, authURL, baseURL string) Tino {
	t.Helper()
	cl := New(authURL, baseURL, "u", "p")
	tok, err := cl.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	cl.SetToken(tok)
	return cl
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// FIX 1: path segments are escaped — no traversal, no query/fragment injection.
func TestFix_PathEscaping(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": true, "data": map[string]any{"status": "cancelled"}})
	})
	cl := newAuthed(t, s.URL, s.URL)

	_, _ = cl.GetUser("../../merchant/settlements/auto-settlement-outbox/invoice/123")
	// The whole value must stay inside one path segment: every "/" escaped to
	// %2F, so ".." can never be resolved as a directory by the server or a proxy.
	if got := c.get(&c.uri); !strings.HasPrefix(got, "/auth/miniapp/") ||
		strings.Contains(strings.TrimPrefix(got, "/auth/miniapp/"), "/") {
		t.Errorf("traversal not contained: %q", got)
	} else {
		t.Logf("OK traversal contained to one segment -> %q", got)
	}

	_, _ = cl.GetUser("abc?admin=true")
	if got := c.get(&c.uri); got != "/auth/miniapp/abc%3Fadmin=true" {
		t.Errorf("query injection: %q", got)
	} else {
		t.Logf("OK query injection blocked -> %q", got)
	}

	_, _ = cl.CancelInvoice("inv1#")
	if got := c.get(&c.uri); !strings.Contains(got, "reason=canceled") {
		t.Errorf("fragment ate the reason param: %q", got)
	} else {
		t.Logf("OK reason param survives '#' -> %q", got)
	}
}

// FIX 2: HTTP 200 + {"status":false} on login must be a hard error, not an
// empty token silently used for every later call.
func TestFix_AuthStatusFalse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/merchant/login" {
			jsonOK(w, map[string]any{"status": false, "message": "invalid credentials"})
			return
		}
		t.Errorf("request reached API with a failed login: %s", r.URL)
		jsonOK(w, map[string]any{"status": true})
	}))
	t.Cleanup(s.Close)

	cl := New(s.URL, s.URL, "bad", "bad")
	_, err := cl.Login(context.Background())
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	t.Logf("OK auth failure surfaced: %v", err)

	// And with no token installed, an API call must not reach the gateway at
	// all — it used to log in implicitly and go out with an empty bearer.
	if _, err := cl.CheckInvoice("inv1"); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

// FIX 3: business failures (status:false) surface as errors on every method.
func TestFix_StatusFalseIsError(t *testing.T) {
	s, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": false, "message": "insufficient funds"})
	})
	cl := newAuthed(t, s.URL, s.URL)

	if _, err := cl.CreateInvoice(&InvoiceRequest{Amount: 1000}); err == nil {
		t.Error("CreateInvoice: expected error")
	} else {
		t.Logf("OK CreateInvoice: %v", err)
	}
	if _, err := cl.CheckInvoice("inv1"); err == nil {
		t.Error("CheckInvoice: expected error")
	} else {
		t.Logf("OK CheckInvoice: %v", err)
	}
	if _, err := cl.CancelInvoice("inv1"); err == nil {
		t.Error("CancelInvoice: expected error")
	} else {
		t.Logf("OK CancelInvoice: %v", err)
	}
}

// FIX 4: an empty gateway message must not produce an empty error string.
func TestFix_NoEmptyErrorText(t *testing.T) {
	s, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": false})
	})
	_, err := newAuthed(t, s.URL, s.URL).GetUser("tok")
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("empty/nil error text: %v", err)
	}
	t.Logf("OK non-empty error: %v", err)
}

// FIX 5: 3xx must be an error, and must not leak the connection.
func TestFix_3xxIsErrorAndNoLeak(t *testing.T) {
	var conns int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/merchant/login" {
			okAuth(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		fmt.Fprint(w, `{"status":false,"message":"pick one"}`)
	})
	s := httptest.NewUnstartedServer(h)
	s.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			atomic.AddInt64(&conns, 1)
		}
	}
	s.Start()
	t.Cleanup(s.Close)

	cl := newAuthed(t, s.URL, s.URL)
	for i := 0; i < 5; i++ {
		if _, err := cl.CheckInvoice(fmt.Sprintf("inv%d", i)); err == nil {
			t.Fatal("300 should be an error")
		}
	}
	n := atomic.LoadInt64(&conns)
	if n > 2 {
		t.Errorf("connection leak: %d new conns for 5 calls", n)
	}
	t.Logf("OK 300 -> error, new TCP conns for 5 calls = %d", n)
}

// FIX 6: a 2xx that is not JSON must be an error, not a silent zero value.
func TestFix_NonJSON200(t *testing.T) {
	s, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>WAF block page</html>")
	})
	if _, err := newAuthed(t, s.URL, s.URL).CreateInvoice(&InvoiceRequest{Amount: 1}); err == nil {
		t.Error("expected decode error")
	} else {
		t.Logf("OK non-JSON 200 rejected: %v", err)
	}
}

// FIX 7: nil request must not be serialised as `null`.
func TestFix_NilBody(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": true})
	})
	if _, err := newAuthed(t, s.URL, s.URL).CreateInvoice(nil); err == nil {
		t.Error("expected error for nil invoice")
	} else {
		t.Logf("OK nil invoice rejected: %v ; body seen by server=%q", err, c.get(&c.body))
	}
}

// FIX 8: the notification password must not appear in the request body.
func TestFix_NotificationCredsNotInBody(t *testing.T) {
	s, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": true})
	})
	_, err := newAuthed(t, s.URL, s.URL).SendNotification(&NotificationRequest{
		Auth:   &BasicAuth{Username: "notif-user", Password: "SUPER-SECRET"},
		App:    "zahii",
		UserID: "u1", Title: "hi", Body: "there",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	body := c.get(&c.body)
	if strings.Contains(body, "SUPER-SECRET") {
		t.Errorf("password leaked into body: %s", body)
	}
	if c.get(&c.authz) == "" {
		t.Error("basic auth header missing")
	}
	t.Logf("OK body=%s", body)
	t.Logf("OK Authorization header still set: %s", c.get(&c.authz))
}

// FIX 9: a rejected (401) token is reported as ErrUnauthorized so its owner
// can replace it. The SDK used to drop and retry from its own cache; it cannot
// any more, because replacing a token it does not own would be invisible to
// whoever does.
func TestFix_RejectedTokenIsReportedAndRecoverable(t *testing.T) {
	var logins, calls int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/merchant/login" {
			n := atomic.AddInt64(&logins, 1)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status": true,
				"data": map[string]any{
					"token":      fmt.Sprintf("TOK%d", n),
					"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		atomic.AddInt64(&calls, 1)
		// reject the first token, accept anything after
		if r.Header.Get("Authorization") == "Bearer TOK1" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"status":false,"message":"token revoked"}`)
			return
		}
		jsonOK(w, map[string]any{"status": true, "data": map[string]any{"invoice_id": "INV-9"}})
	}))
	t.Cleanup(s.Close)

	cl := newAuthed(t, s.URL, s.URL) // installs TOK1

	_, err := cl.CreateInvoice(&InvoiceRequest{Amount: 1})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	// The caller's half of the contract: log in again, install, retry once.
	tok, err := cl.Login(context.Background())
	if err != nil {
		t.Fatalf("re-login: %v", err)
	}
	cl.SetToken(tok)

	res, err := cl.CreateInvoice(&InvoiceRequest{Amount: 1})
	if err != nil {
		t.Fatalf("expected recovery after replacing the token, got %v", err)
	}
	t.Logf("OK recovered after 401: invoice=%s (logins=%d, api calls=%d)",
		res.Data.InvoiceID, atomic.LoadInt64(&logins), atomic.LoadInt64(&calls))
}

// Concurrency must stay race-free.
func TestFix_Concurrent(t *testing.T) {
	s, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": true, "data": map[string]any{}})
	})
	cl := newAuthed(t, s.URL, s.URL)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = cl.CheckInvoice(fmt.Sprintf("inv%d", i))
			_, _ = cl.GetUser("tok")
		}(i)
	}
	wg.Wait()
}
