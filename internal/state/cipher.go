package state

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/maine/vietnam_bot_news/internal/news"
)

const encryptedIDPrefix = "v1:"

// IDCipher encrypts Telegram chat IDs before they are written to state.
type IDCipher struct {
	gcm cipher.AEAD
}

// NewIDCipher creates an AES-256-GCM cipher from a secret passphrase.
func NewIDCipher(passphrase string) (*IDCipher, error) {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return nil, fmt.Errorf("state encryption key is empty")
	}

	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create state encryption cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create state encryption GCM: %w", err)
	}

	return &IDCipher{gcm: gcm}, nil
}

// Encrypt returns a versioned, base64url-encoded AES-GCM value.
func (c *IDCipher) Encrypt(chatID string) (string, error) {
	if c == nil || c.gcm == nil {
		return "", fmt.Errorf("state encryption cipher is not configured")
	}
	if chatID == "" {
		return "", fmt.Errorf("chat ID is empty")
	}

	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate state encryption nonce: %w", err)
	}
	ciphertext := c.gcm.Seal(nonce, nonce, []byte(chatID), nil)
	return encryptedIDPrefix + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt returns the chat ID and reports whether the value used the legacy
// plaintext format. Legacy numeric IDs are accepted only for migration.
func (c *IDCipher) Decrypt(value string) (chatID string, legacy bool, err error) {
	if c == nil || c.gcm == nil {
		return "", false, fmt.Errorf("state encryption cipher is not configured")
	}
	if value == "" {
		return "", false, fmt.Errorf("stored chat ID is empty")
	}

	if !strings.HasPrefix(value, encryptedIDPrefix) {
		if _, parseErr := strconv.ParseInt(value, 10, 64); parseErr != nil {
			return "", false, fmt.Errorf("invalid legacy chat ID: %w", parseErr)
		}
		return value, true, nil
	}

	encoded := strings.TrimPrefix(value, encryptedIDPrefix)
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false, fmt.Errorf("decode encrypted chat ID: %w", err)
	}
	if len(data) < c.gcm.NonceSize() {
		return "", false, fmt.Errorf("encrypted chat ID is too short")
	}

	nonce := data[:c.gcm.NonceSize()]
	chatIDBytes, err := c.gcm.Open(nil, nonce, data[c.gcm.NonceSize():], nil)
	if err != nil {
		return "", false, fmt.Errorf("decrypt chat ID: %w", err)
	}
	chatID = string(chatIDBytes)
	if _, err := strconv.ParseInt(chatID, 10, 64); err != nil {
		return "", false, fmt.Errorf("decrypted chat ID is invalid: %w", err)
	}
	return chatID, false, nil
}

// MigrateRecipients converts legacy plaintext recipient IDs and removes names
// by returning the state in its current persisted representation.
func (c *IDCipher) MigrateRecipients(state news.State) (news.State, int, error) {
	migrated := 0
	for i := range state.Recipients {
		if state.Recipients[i].ChatID == "" {
			continue
		}
		chatID, legacy, err := c.Decrypt(state.Recipients[i].ChatID)
		if err != nil {
			return state, migrated, fmt.Errorf("recipient %d: %w", i, err)
		}
		if !legacy {
			continue
		}
		encrypted, err := c.Encrypt(chatID)
		if err != nil {
			return state, migrated, fmt.Errorf("recipient %d: %w", i, err)
		}
		state.Recipients[i].ChatID = encrypted
		migrated++
	}
	return state, migrated, nil
}
