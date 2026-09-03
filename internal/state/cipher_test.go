package state

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
)

func TestIDCipher_EncryptDecrypt(t *testing.T) {
	cipher := newTestCipher(t)

	encrypted, err := cipher.Encrypt("-1001234567890")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if encrypted == "-1001234567890" || !strings.HasPrefix(encrypted, encryptedIDPrefix) {
		t.Fatalf("Encrypt() = %q, want versioned ciphertext", encrypted)
	}

	decrypted, legacy, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if legacy {
		t.Error("Decrypt() reported encrypted value as legacy")
	}
	if decrypted != "-1001234567890" {
		t.Errorf("Decrypt() = %q, want %q", decrypted, "-1001234567890")
	}
}

func TestIDCipher_RejectsTamperedCiphertext(t *testing.T) {
	cipher := newTestCipher(t)
	encrypted, err := cipher.Encrypt("123")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encrypted, encryptedIDPrefix))
	if err != nil {
		t.Fatalf("decode encrypted test value: %v", err)
	}
	data[len(data)-1] ^= 1
	tampered := encryptedIDPrefix + base64.RawURLEncoding.EncodeToString(data)
	if _, _, err := cipher.Decrypt(tampered); err == nil {
		t.Error("Decrypt() should reject tampered ciphertext")
	}
}

func TestIDCipher_DecryptsLegacyID(t *testing.T) {
	cipher := newTestCipher(t)

	decrypted, legacy, err := cipher.Decrypt("12345")
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !legacy || decrypted != "12345" {
		t.Errorf("Decrypt() = (%q, %v), want (%q, true)", decrypted, legacy, "12345")
	}
}

func TestIDCipher_MigrateRecipients(t *testing.T) {
	cipher := newTestCipher(t)
	input := news.State{
		Recipients: []news.StoredRecipient{
			{ChatID: "123", UpdatedAt: testTime()},
			{ChatID: "v1:already-encrypted", UpdatedAt: testTime()},
		},
	}
	validEncrypted, err := cipher.Encrypt("456")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	input.Recipients[1].ChatID = validEncrypted

	migrated, count, err := cipher.MigrateRecipients(input)
	if err != nil {
		t.Fatalf("MigrateRecipients() error = %v", err)
	}
	if count != 1 {
		t.Errorf("MigrateRecipients() count = %d, want 1", count)
	}
	if migrated.Recipients[0].ChatID == "123" {
		t.Error("MigrateRecipients() left legacy ID unencrypted")
	}
	if migrated.Recipients[1].ChatID != validEncrypted {
		t.Error("MigrateRecipients() changed an already encrypted ID")
	}
}

func newTestCipher(t *testing.T) *IDCipher {
	t.Helper()
	cipher, err := NewIDCipher("test-only-state-encryption-passphrase")
	if err != nil {
		t.Fatalf("NewIDCipher() error = %v", err)
	}
	return cipher
}

func testTime() time.Time {
	return time.Unix(0, 0).UTC()
}
