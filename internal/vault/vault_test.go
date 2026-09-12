package vault

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKEK(t *testing.T) []byte {
	t.Helper()
	kek := make([]byte, DEKSize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	return kek
}

func TestMakeAAD(t *testing.T) {
	got := MakeAAD("acc-1", "password")
	want := append(append([]byte("acc-1"), 0x00), []byte("password")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("MakeAAD = %q, want %q", got, want)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	kek := testKEK(t)
	plain := []byte("s3cret-value")
	env, err := Seal(plain, "account-uuid", "password", kek)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(env.Nonce) != NonceSize {
		t.Fatalf("nonce len = %d", len(env.Nonce))
	}
	if len(env.Ciphertext) < TagSize {
		t.Fatalf("ciphertext too short")
	}
	if len(env.WrappedDEK) < NonceSize+TagSize+DEKSize {
		t.Fatalf("wrapped_dek too short")
	}

	got, err := Open(env, "account-uuid", "password", kek)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plaintext = %q, want %q", got, plain)
	}
}

func TestOpenWrongAADFails(t *testing.T) {
	kek := testKEK(t)
	env, err := Seal([]byte("payload"), "acc-a", "password", kek)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	t.Run("wrong account_id", func(t *testing.T) {
		if _, err := Open(env, "acc-b", "password", kek); err == nil {
			t.Fatal("expected error for wrong account_id AAD")
		}
	})
	t.Run("wrong auth_type", func(t *testing.T) {
		if _, err := Open(env, "acc-a", "ssh_private_key", kek); err == nil {
			t.Fatal("expected error for wrong auth_type AAD")
		}
	})
}

func TestSealUsesFreshNonce(t *testing.T) {
	kek := testKEK(t)
	plain := []byte("same")
	a, err := Seal(plain, "acc", "password", kek)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Seal(plain, "acc", "password", kek)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Fatal("expected distinct nonces across Seal calls")
	}
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("expected distinct ciphertext across Seal calls")
	}
}

func TestRewrapThenDecryptSucceeds(t *testing.T) {
	fromKEK := testKEK(t)
	toKEK := testKEK(t)
	accountID := "acc-rewrap"
	authType := "password"
	plain := []byte("rotate-me")

	env, err := Seal(plain, accountID, authType, fromKEK)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	origNonce := append([]byte(nil), env.Nonce...)
	origCT := append([]byte(nil), env.Ciphertext...)

	newWrapped, err := RewrapDEK(env.WrappedDEK, fromKEK, toKEK)
	if err != nil {
		t.Fatalf("RewrapDEK: %v", err)
	}
	if bytes.Equal(newWrapped, env.WrappedDEK) {
		t.Fatal("expected wrapped_dek to change after rewrap")
	}

	// Classic rewrap must not alter payload nonce/ciphertext.
	if !bytes.Equal(env.Nonce, origNonce) || !bytes.Equal(env.Ciphertext, origCT) {
		t.Fatal("Seal envelope nonce/ciphertext mutated unexpectedly")
	}

	env.WrappedDEK = newWrapped

	if _, err := Open(env, accountID, authType, fromKEK); err == nil {
		t.Fatal("expected Open with old KEK to fail after rewrap")
	}

	got, err := Open(env, accountID, authType, toKEK)
	if err != nil {
		t.Fatalf("Open after rewrap: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plaintext after rewrap = %q, want %q", got, plain)
	}

	// Same AAD still required after rewrap.
	if _, err := Open(env, "other-acc", authType, toKEK); err == nil {
		t.Fatal("expected wrong AAD to fail after rewrap")
	}
}

func TestRewrapRejectsBadKEK(t *testing.T) {
	kek := testKEK(t)
	env, err := Seal([]byte("x"), "a", "password", kek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RewrapDEK(env.WrappedDEK, kek[:16], kek); err == nil {
		t.Fatal("expected short fromKEK error")
	}
	if _, err := RewrapDEK(env.WrappedDEK, kek, make([]byte, 16)); err == nil {
		t.Fatal("expected short toKEK error")
	}
	wrong := testKEK(t)
	if _, err := RewrapDEK(env.WrappedDEK, wrong, kek); err == nil {
		t.Fatal("expected unwrap failure with wrong fromKEK")
	}
}
