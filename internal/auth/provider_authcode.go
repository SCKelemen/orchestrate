package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// PKCE helpers per RFC 7636 - Proof Key for Code Exchange
// https://datatracker.ietf.org/doc/html/rfc7636

var (
	ErrPKCEMethodNotSupported = errors.New("only S256 code_challenge_method is supported")
	ErrPKCEVerifyFailed       = errors.New("PKCE verification failed")
)

// GenerateCodeVerifier creates a cryptographically random code verifier.
// RFC 7636 Section 4.1: 43-128 character URL-safe string
// https://datatracker.ietf.org/doc/html/rfc7636#section-4.1
func GenerateCodeVerifier() (string, error) {
	b := make([]byte, 32) // 32 bytes = 43 base64url chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CodeChallengeS256 derives the S256 code challenge from a verifier.
// RFC 7636 Section 4.2: code_challenge = BASE64URL(SHA256(code_verifier))
// https://datatracker.ietf.org/doc/html/rfc7636#section-4.2
func CodeChallengeS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// VerifyPKCE verifies the code verifier against the stored challenge.
// Only S256 method is supported per security best practices.
func VerifyPKCE(method, challenge, verifier string) error {
	if method != "S256" {
		return ErrPKCEMethodNotSupported
	}
	computed := CodeChallengeS256(verifier)
	if computed != challenge {
		return ErrPKCEVerifyFailed
	}
	return nil
}

// GenerateAuthCode creates a cryptographically random authorization code.
func GenerateAuthCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
