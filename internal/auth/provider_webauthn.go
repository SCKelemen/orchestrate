package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnConfig holds configuration for the WebAuthn provider.
type WebAuthnConfig struct {
	RPDisplayName string // Relying party display name
	RPID          string // Relying party ID (domain)
	RPOrigins     []string
}

// WebAuthnProvider manages WebAuthn registration and login ceremonies.
type WebAuthnProvider struct {
	wan *webauthn.WebAuthn
}

// NewWebAuthnProvider creates a WebAuthn provider.
func NewWebAuthnProvider(cfg WebAuthnConfig) (*WebAuthnProvider, error) {
	wan, err := webauthn.New(&webauthn.Config{
		RPDisplayName: cfg.RPDisplayName,
		RPID:          cfg.RPID,
		RPOrigins:     cfg.RPOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("init webauthn: %w", err)
	}
	return &WebAuthnProvider{wan: wan}, nil
}

// WebAuthnUser implements the webauthn.User interface.
type WebAuthnUser struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

func (u *WebAuthnUser) WebAuthnID() []byte                         { return u.ID }
func (u *WebAuthnUser) WebAuthnName() string                       { return u.Name }
func (u *WebAuthnUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// BeginRegistration starts a WebAuthn registration ceremony.
func (p *WebAuthnProvider) BeginRegistration(user *WebAuthnUser) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	return p.wan.BeginRegistration(user)
}

// FinishRegistration completes a WebAuthn registration ceremony.
func (p *WebAuthnProvider) FinishRegistration(user *WebAuthnUser, session webauthn.SessionData, r *http.Request) (*webauthn.Credential, error) {
	return p.wan.FinishRegistration(user, session, r)
}

// BeginLogin starts a WebAuthn login ceremony.
func (p *WebAuthnProvider) BeginLogin(user *WebAuthnUser) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return p.wan.BeginLogin(user)
}

// FinishLogin completes a WebAuthn login ceremony.
func (p *WebAuthnProvider) FinishLogin(user *WebAuthnUser, session webauthn.SessionData, r *http.Request) (*webauthn.Credential, error) {
	return p.wan.FinishLogin(user, session, r)
}

// MarshalSessionData serializes session data for storage.
func MarshalSessionData(sd *webauthn.SessionData) ([]byte, error) {
	return json.Marshal(sd)
}

// UnmarshalSessionData deserializes session data from storage.
func UnmarshalSessionData(data []byte) (*webauthn.SessionData, error) {
	var sd webauthn.SessionData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

// MarshalCredential serializes a WebAuthn credential for storage.
func MarshalCredential(cred *webauthn.Credential) ([]byte, error) {
	return json.Marshal(cred)
}

// UnmarshalCredential deserializes a WebAuthn credential from storage.
func UnmarshalCredential(data []byte) (*webauthn.Credential, error) {
	var cred webauthn.Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	return &cred, nil
}

// WebAuthnSessionStore provides in-memory storage for WebAuthn ceremony sessions.
// Sessions are short-lived (ceremony duration) and don't need persistence.
type WebAuthnSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*webauthnSessionEntry
}

type webauthnSessionEntry struct {
	data      *webauthn.SessionData
	expiresAt time.Time
}

// NewWebAuthnSessionStore creates a new in-memory session store.
func NewWebAuthnSessionStore() *WebAuthnSessionStore {
	return &WebAuthnSessionStore{
		sessions: make(map[string]*webauthnSessionEntry),
	}
}

// Save stores a WebAuthn session with a 5-minute expiry.
func (s *WebAuthnSessionStore) Save(key string, data *webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = &webauthnSessionEntry{
		data:      data,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
}

// Get retrieves and removes a WebAuthn session.
func (s *WebAuthnSessionStore) Get(key string) (*webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[key]
	if !ok {
		return nil, false
	}
	delete(s.sessions, key)
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}
