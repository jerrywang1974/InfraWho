package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const (
	NonceSize = 12 // AES-GCM nonce (96-bit)
	DEKSize   = 32 // AES-256 key
	TagSize   = 16 // AES-GCM tag
)

// Envelope holds sealed secret material for secret_payloads.
// Ciphertext is ciphertext||tag (GCM tag is the last TagSize bytes).
// WrappedDEK is wrapNonce||ciphertext||tag (NonceSize bytes of nonce, then sealed DEK).
type Envelope struct {
	Nonce      []byte
	Ciphertext []byte
	WrappedDEK []byte
}

// MakeAAD builds AAD as utf8(accountID) || 0x00 || utf8(authType).
// key_version is not part of AAD.
func MakeAAD(accountID, authType string) []byte {
	aid := []byte(accountID)
	at := []byte(authType)
	out := make([]byte, 0, len(aid)+1+len(at))
	out = append(out, aid...)
	out = append(out, 0x00)
	out = append(out, at...)
	return out
}

// Seal encrypts plaintext with a fresh random DEK under AES-256-GCM and wraps the DEK with kek.
func Seal(plaintext []byte, accountID, authType string, kek []byte) (*Envelope, error) {
	if err := checkKEK(kek); err != nil {
		return nil, err
	}

	dek := make([]byte, DEKSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}
	defer zeroBytes(dek)

	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	aad := MakeAAD(accountID, authType)
	ciphertext, err := gcmSeal(dek, nonce, plaintext, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}

	wrapped, err := wrapDEK(dek, kek)
	if err != nil {
		return nil, err
	}

	return &Envelope{
		Nonce:      nonce,
		Ciphertext: ciphertext,
		WrappedDEK: wrapped,
	}, nil
}

// Open unwraps the DEK with kek and decrypts the payload using AAD from accountID/authType.
func Open(env *Envelope, accountID, authType string, kek []byte) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("nil envelope")
	}
	if err := checkKEK(kek); err != nil {
		return nil, err
	}
	if len(env.Nonce) != NonceSize {
		return nil, fmt.Errorf("nonce length %d, want %d", len(env.Nonce), NonceSize)
	}
	if len(env.Ciphertext) < TagSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	dek, err := unwrapDEK(env.WrappedDEK, kek)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dek)

	aad := MakeAAD(accountID, authType)
	plain, err := gcmOpen(dek, env.Nonce, env.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt payload: %w", err)
	}
	return plain, nil
}

// RewrapDEK returns a new wrapped_dek blob under toKEK.
func RewrapDEK(wrappedDEK, fromKEK, toKEK []byte) ([]byte, error) {
	if err := checkKEK(fromKEK); err != nil {
		return nil, fmt.Errorf("from KEK: %w", err)
	}
	if err := checkKEK(toKEK); err != nil {
		return nil, fmt.Errorf("to KEK: %w", err)
	}
	dek, err := unwrapDEK(wrappedDEK, fromKEK)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dek)
	return wrapDEK(dek, toKEK)
}

func wrapDEK(dek, kek []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, fmt.Errorf("DEK length %d, want %d", len(dek), DEKSize)
	}
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate wrap nonce: %w", err)
	}
	ct, err := gcmSeal(kek, nonce, dek, nil)
	if err != nil {
		return nil, fmt.Errorf("wrap DEK: %w", err)
	}
	out := make([]byte, 0, NonceSize+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func unwrapDEK(wrapped, kek []byte) ([]byte, error) {
	if len(wrapped) < NonceSize+TagSize+DEKSize {
		return nil, fmt.Errorf("wrapped_dek too short")
	}
	nonce := wrapped[:NonceSize]
	ct := wrapped[NonceSize:]
	dek, err := gcmOpen(kek, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("unwrap DEK: %w", err)
	}
	if len(dek) != DEKSize {
		zeroBytes(dek)
		return nil, fmt.Errorf("unwrapped DEK length %d, want %d", len(dek), DEKSize)
	}
	return dek, nil
}

func gcmSeal(key, nonce, plaintext, aad []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce length %d, want %d", len(nonce), aead.NonceSize())
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

func gcmOpen(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce length %d, want %d", len(nonce), aead.NonceSize())
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return aead, nil
}

func checkKEK(kek []byte) error {
	if len(kek) != DEKSize {
		return fmt.Errorf("KEK length %d, want %d", len(kek), DEKSize)
	}
	return nil
}

// zeroBytes best-effort clears b; Go cannot guarantee wipe against GC/compiler.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
