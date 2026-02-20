package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Device flow helpers per RFC 8628 - OAuth 2.0 Device Authorization Grant
// https://datatracker.ietf.org/doc/html/rfc8628

// userCodeAlphabet excludes confusable characters: 0/O, I/l, 1
// RFC 8628 Section 6.1 recommends using a limited character set
// https://datatracker.ietf.org/doc/html/rfc8628#section-6.1
const userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateDeviceCode creates a cryptographically random device code.
func GenerateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateUserCode creates an 8-character user code formatted as XXXX-XXXX.
// Uses a reduced alphabet that excludes confusable characters.
func GenerateUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i := range code {
		code[i] = userCodeAlphabet[int(b[i])%len(userCodeAlphabet)]
	}
	return string(code[:4]) + "-" + string(code[4:]), nil
}

// NormalizeUserCode strips hyphens/spaces and uppercases for comparison.
func NormalizeUserCode(code string) string {
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return strings.ToUpper(code)
}
