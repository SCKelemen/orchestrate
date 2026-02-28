package auth

import (
	"testing"
)

func TestGenerateAuthReqID(t *testing.T) {
	id, err := GenerateAuthReqID()
	if err != nil {
		t.Fatalf("GenerateAuthReqID: %v", err)
	}
	// 32 bytes = 64 hex chars.
	if len(id) != 64 {
		t.Errorf("auth_req_id length = %d, want 64", len(id))
	}
}

func TestGenerateAuthReqID_Unique(t *testing.T) {
	id1, _ := GenerateAuthReqID()
	id2, _ := GenerateAuthReqID()
	if id1 == id2 {
		t.Error("two auth_req_ids should not be equal")
	}
}
