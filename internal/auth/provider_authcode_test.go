package auth

import (
	"testing"
)

func TestPKCE_S256_Roundtrip(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}

	// RFC 7636 Section 4.1: verifier is 43-128 URL-safe characters.
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length = %d, want 43-128", len(verifier))
	}

	challenge := CodeChallengeS256(verifier)
	if challenge == "" {
		t.Fatal("challenge is empty")
	}
	if challenge == verifier {
		t.Error("challenge should differ from verifier")
	}

	if err := VerifyPKCE("S256", challenge, verifier); err != nil {
		t.Fatalf("VerifyPKCE: %v", err)
	}
}

func TestPKCE_S256_WrongVerifier(t *testing.T) {
	verifier, _ := GenerateCodeVerifier()
	challenge := CodeChallengeS256(verifier)

	err := VerifyPKCE("S256", challenge, "wrong-verifier")
	if err != ErrPKCEVerifyFailed {
		t.Errorf("expected ErrPKCEVerifyFailed, got %v", err)
	}
}

func TestPKCE_PlainMethodRejected(t *testing.T) {
	err := VerifyPKCE("plain", "challenge", "verifier")
	if err != ErrPKCEMethodNotSupported {
		t.Errorf("expected ErrPKCEMethodNotSupported, got %v", err)
	}
}

func TestPKCE_EmptyMethodRejected(t *testing.T) {
	err := VerifyPKCE("", "challenge", "verifier")
	if err != ErrPKCEMethodNotSupported {
		t.Errorf("expected ErrPKCEMethodNotSupported, got %v", err)
	}
}

func TestCodeChallengeS256_Deterministic(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	c1 := CodeChallengeS256(verifier)
	c2 := CodeChallengeS256(verifier)
	if c1 != c2 {
		t.Errorf("same verifier produced different challenges: %q vs %q", c1, c2)
	}
}

func TestGenerateCodeVerifier_Unique(t *testing.T) {
	v1, _ := GenerateCodeVerifier()
	v2, _ := GenerateCodeVerifier()
	if v1 == v2 {
		t.Error("two verifiers should not be equal")
	}
}

func TestGenerateAuthCode_LengthAndUniqueness(t *testing.T) {
	c1, err := GenerateAuthCode()
	if err != nil {
		t.Fatalf("GenerateAuthCode: %v", err)
	}
	if len(c1) == 0 {
		t.Fatal("auth code is empty")
	}

	c2, _ := GenerateAuthCode()
	if c1 == c2 {
		t.Error("two auth codes should not be equal")
	}
}

// RFC 7636 Appendix B test vector.
// https://datatracker.ietf.org/doc/html/rfc7636#appendix-B
func TestCodeChallengeS256_RFC7636Vector(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	got := CodeChallengeS256(verifier)
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}
