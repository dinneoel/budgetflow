package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash %q does not use argon2id encoding", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Errorf("correct password: ok=%v err=%v, want true nil", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil || ok {
		t.Errorf("wrong password: ok=%v err=%v, want false nil", ok, err)
	}
}

func TestHashPasswordUniqueSalts(t *testing.T) {
	h1, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password are identical: salt is not random")
	}
}

func TestVerifyPasswordMalformedHash(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$m=19456,t=2,p=1$notb64!$x", "$bcrypt$whatever"} {
		if ok, err := VerifyPassword(bad, "pw"); err == nil || ok {
			t.Errorf("VerifyPassword(%q) = %v, %v; want false, error", bad, ok, err)
		}
	}
}

func TestNewTokenUnique(t *testing.T) {
	raw1, hash1, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	raw2, hash2, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw1 == raw2 || hash1 == hash2 {
		t.Error("consecutive tokens are identical")
	}
	if hashToken(raw1) != hash1 {
		t.Error("hashToken(raw) does not match returned hash")
	}
}
