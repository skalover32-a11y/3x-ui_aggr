package ops

import "testing"

func TestPublicTokenHashing(t *testing.T) {
	token, hash, err := generatePublicToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	got := hashPublicToken(token)
	if got != hash {
		t.Fatalf("hash mismatch: %s != %s", got, hash)
	}
}
