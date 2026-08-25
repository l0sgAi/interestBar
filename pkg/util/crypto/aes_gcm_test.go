package crypto

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	const key = "test-data-key"
	cases := []string{"sk-abc123xyz789", "", "中文密钥内容 🔑"}
	for _, plain := range cases {
		enc, err := Encrypt(key, plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		got, err := Decrypt(key, enc)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", enc, err)
		}
		if got != plain {
			t.Fatalf("round trip mismatch: got %q want %q", got, plain)
		}
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	enc, err := Encrypt("key-a", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt("key-b", enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestEmptyKeyFails(t *testing.T) {
	if _, err := Encrypt("", "x"); err != ErrEmptyKey {
		t.Fatalf("want ErrEmptyKey, got %v", err)
	}
}

func TestInvalidCiphertext(t *testing.T) {
	if _, err := Decrypt("k", "not-base64!!!"); err != ErrInvalidCiphertext {
		t.Fatalf("want ErrInvalidCiphertext, got %v", err)
	}
	if _, err := Decrypt("k", "AAAA"); err != ErrInvalidCiphertext {
		t.Fatalf("short ciphertext: want ErrInvalidCiphertext, got %v", err)
	}
}

func TestMask(t *testing.T) {
	if got := Mask("sk-abc123xyz789"); got != "sk-****z789" {
		t.Fatalf("got %q", got)
	}
	if got := Mask("short"); got != "****" {
		t.Fatalf("short value should be fully masked, got %q", got)
	}
}
