package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// CIBA helpers per OpenID Connect Client-Initiated Backchannel Authentication Flow
// https://openid.net/specs/openid-client-initiated-backchannel-authentication-core-1_0.html

// GenerateAuthReqID creates a cryptographically random auth_req_id for CIBA.
func GenerateAuthReqID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
