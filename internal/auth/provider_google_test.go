package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleExchange_Success(t *testing.T) {
	user := GoogleUser{
		Sub:   "1234567890",
		Name:  "Test User",
		Email: "test@gmail.com",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ya29.test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(user)
	}))
	defer srv.Close()

	g := &GoogleExchange{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	got, err := g.Exchange("ya29.test")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.Sub != "1234567890" {
		t.Errorf("Sub = %q, want %q", got.Sub, "1234567890")
	}
	if got.Email != "test@gmail.com" {
		t.Errorf("Email = %q, want %q", got.Email, "test@gmail.com")
	}
}

func TestGoogleExchange_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()

	g := &GoogleExchange{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	_, err := g.Exchange("bad-token")
	if err == nil {
		t.Fatal("expected error for unauthorized token")
	}
}
