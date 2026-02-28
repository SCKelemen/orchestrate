package auth

import (
	"strings"
	"testing"
)

func TestGenerateDeviceCode(t *testing.T) {
	code, err := GenerateDeviceCode()
	if err != nil {
		t.Fatalf("GenerateDeviceCode: %v", err)
	}
	// 32 bytes = 64 hex chars.
	if len(code) != 64 {
		t.Errorf("device code length = %d, want 64", len(code))
	}
	// Should be lowercase hex.
	for _, c := range code {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("unexpected character %c in device code", c)
			break
		}
	}
}

func TestGenerateDeviceCode_Unique(t *testing.T) {
	c1, _ := GenerateDeviceCode()
	c2, _ := GenerateDeviceCode()
	if c1 == c2 {
		t.Error("two device codes should not be equal")
	}
}

func TestGenerateUserCode_Format(t *testing.T) {
	code, err := GenerateUserCode()
	if err != nil {
		t.Fatalf("GenerateUserCode: %v", err)
	}

	// Should be XXXX-XXXX format.
	if len(code) != 9 {
		t.Fatalf("user code length = %d, want 9 (XXXX-XXXX)", len(code))
	}
	if code[4] != '-' {
		t.Errorf("expected hyphen at position 4, got %c", code[4])
	}
}

func TestGenerateUserCode_Alphabet(t *testing.T) {
	// Generate several codes and check all characters are from the allowed alphabet.
	for i := 0; i < 100; i++ {
		code, err := GenerateUserCode()
		if err != nil {
			t.Fatalf("GenerateUserCode: %v", err)
		}
		raw := strings.ReplaceAll(code, "-", "")
		for _, c := range raw {
			if !strings.ContainsRune(userCodeAlphabet, c) {
				t.Errorf("character %c not in allowed alphabet", c)
			}
		}
	}
}

func TestGenerateUserCode_NoConfusableChars(t *testing.T) {
	// Verify confusable characters are absent from the alphabet.
	confusable := "0OIl1"
	for _, c := range confusable {
		if strings.ContainsRune(userCodeAlphabet, c) {
			t.Errorf("alphabet contains confusable character %c", c)
		}
	}
}

func TestNormalizeUserCode(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"ABCD-EFGH", "ABCDEFGH"},
		{"abcd-efgh", "ABCDEFGH"},
		{"abcd efgh", "ABCDEFGH"},
		{"AbCd EfGh", "ABCDEFGH"},
		{"ABCDEFGH", "ABCDEFGH"},
		{"  ab-cd  ", "ABCD"},
	}

	for _, tc := range cases {
		got := NormalizeUserCode(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
