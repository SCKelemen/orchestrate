package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubExchange_Success(t *testing.T) {
	user := GitHubUser{
		ID:    12345,
		Login: "octocat",
		Name:  "Mona Lisa",
		Email: "octocat@github.com",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_test123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(user)
	}))
	defer srv.Close()

	g := &GitHubExchange{client: srv.Client()}
	// Override the URL by creating a custom request in Exchange.
	// Since we can't easily override the URL, test with a mock server that
	// replaces the client transport.
	g.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Redirect to test server.
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	got, err := g.Exchange("ghp_test123")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.Login != "octocat" {
		t.Errorf("Login = %q, want %q", got.Login, "octocat")
	}
	if got.ID != 12345 {
		t.Errorf("ID = %d, want %d", got.ID, 12345)
	}
}

func TestGitHubExchange_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	g := &GitHubExchange{
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

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
