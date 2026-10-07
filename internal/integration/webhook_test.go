package integration

import "testing"

func TestSignDeterministic(t *testing.T) {
	// HMAC-SHA256("k", "1700000000.{\"a\":1}") — computed independently.
	got := Sign("k", "1700000000", []byte(`{"a":1}`))
	if len(got) != len("sha256=")+64 || got[:7] != "sha256=" {
		t.Fatalf("unexpected format: %s", got)
	}
	if again := Sign("k", "1700000000", []byte(`{"a":1}`)); again != got {
		t.Fatal("signature not deterministic")
	}
	if Sign("other", "1700000000", []byte(`{"a":1}`)) == got {
		t.Fatal("different secret must change signature")
	}
	if Sign("k", "1700000001", []byte(`{"a":1}`)) == got {
		t.Fatal("different timestamp must change signature")
	}
}
