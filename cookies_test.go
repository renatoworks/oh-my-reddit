package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestJoinAndSession(t *testing.T) {
	pairs := map[string]string{"token_v2": "abc", "reddit_session": "xyz", "loid": "1"}
	got := joinCookies(pairs)
	want := "loid=1; reddit_session=xyz; token_v2=abc" // sorted, "; " joined
	if got != want {
		t.Errorf("joinCookies = %q, want %q", got, want)
	}
	if !hasSession(pairs) {
		t.Errorf("hasSession should be true with reddit_session")
	}
	if hasSession(map[string]string{"loid": "1"}) {
		t.Errorf("hasSession should be false without a session cookie")
	}
}

// encChromeV10 mirrors Chrome's macOS cookie encryption so we can round-trip the
// decryptor: AES-128-CBC, IV = 16 spaces, "v10" prefix, PKCS7 padding, and the
// optional 32-byte host hash that recent Chrome prepends to the value.
func encChromeV10(t *testing.T, plaintext string, key []byte, withHostHash bool) []byte {
	t.Helper()
	data := []byte(plaintext)
	if withHostHash {
		data = append(bytes.Repeat([]byte{0xff}, 32), data...) // non-UTF-8 fake hash
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(ct, data)
	return append([]byte("v10"), ct...)
}

func TestDecryptChromeV10(t *testing.T) {
	key := make([]byte, 16) // a fixed 16-byte AES key is enough to round-trip

	// Recent Chrome: value carries the 32-byte host hash, which must be stripped.
	enc := encChromeV10(t, "reddit_session=abc123", key, true)
	if got, ok := decryptChromiumV10(enc, key); !ok || got != "reddit_session=abc123" {
		t.Errorf("with host hash: got %q (ok=%v), want reddit_session=abc123", got, ok)
	}

	// Older Chrome: no host hash prefix.
	enc = encChromeV10(t, "token_v2=xyz", key, false)
	if got, ok := decryptChromiumV10(enc, key); !ok || got != "token_v2=xyz" {
		t.Errorf("no host hash: got %q (ok=%v), want token_v2=xyz", got, ok)
	}

	// Non-v10 input is rejected, not mis-decrypted.
	if _, ok := decryptChromiumV10([]byte("v20whatever"), key); ok {
		t.Error("non-v10 prefix should not decrypt")
	}

	// A value that decrypts to non-text (a wrong key, or an unknown format) must
	// be reported as a failure, not handed back as an empty/garbage "cookie" —
	// otherwise "found the session but couldn't decrypt it" looks like "no session".
	garbage := encChromeV10(t, string([]byte{0xff, 0xfe, 0x00, 0x80}), key, false)
	if got, ok := decryptChromiumV10(garbage, key); ok {
		t.Errorf("non-UTF-8 plaintext should fail to decrypt, got %q (ok=%v)", got, ok)
	}

	// A wrong key produces garbage and must likewise be rejected.
	wrongKey := make([]byte, 16)
	wrongKey[0] = 0x42
	enc = encChromeV10(t, "reddit_session=this_is_a_reasonably_long_value_xyz", key, false)
	if got, ok := decryptChromiumV10(enc, wrongKey); ok {
		t.Errorf("wrong key should fail to decrypt, got %q (ok=%v)", got, ok)
	}
}
