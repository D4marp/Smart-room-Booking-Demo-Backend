package utils

import "testing"

func TestHashPasswordAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" || hash == "s3cret-pass" {
		t.Fatalf("expected hashed password, got %q", hash)
	}

	if !CheckPassword("s3cret-pass", hash) {
		t.Fatal("expected CheckPassword to succeed with correct password")
	}

	if CheckPassword("wrong-pass", hash) {
		t.Fatal("expected CheckPassword to fail with incorrect password")
	}
}

func TestHashPasswordProducesDifferentHashesForSameInput(t *testing.T) {
	hash1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	hash2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if hash1 == hash2 {
		t.Fatal("expected bcrypt salting to produce different hashes for identical passwords")
	}
}

func TestCheckPasswordWithInvalidHash(t *testing.T) {
	if CheckPassword("any-password", "not-a-valid-bcrypt-hash") {
		t.Fatal("expected CheckPassword to fail gracefully with malformed hash")
	}
}
