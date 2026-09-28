// Package secrets — шифрование LLM-токенов пользователей «в покое».
// Мастер-ключ: 32 случайных байта в $AGCTL_HOME/secrets.key (0600), создаётся
// при первом запуске. Секреты никогда не попадают в git/логи/образы.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrNoMasterKey = errors.New("secrets: мастер-ключ отсутствует")

// Manager — держит мастер-ключ AES-256-GCM.
type Manager struct {
	aead cipher.AEAD
}

// EnsureMasterKey создаёт файл мастер-ключа, если его нет, и возвращает путь.
func EnsureMasterKey(home string) (string, error) {
	p := filepath.Join(home, "secrets.key")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("secrets: rand: %w", err)
	}
	if err := os.WriteFile(p, key, 0o600); err != nil {
		return "", fmt.Errorf("secrets: write key: %w", err)
	}
	return p, nil
}

// New загружает мастер-ключ и готовит AEAD.
func New(keyPath string) (*Manager, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, errors.Join(ErrNoMasterKey, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: ключ %s: ждали 32 байта, получили %d", keyPath, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Manager{aead: aead}, nil
}

// Encrypt → base64(nonce || ciphertext).
func (m *Manager) Encrypt(plain string) (string, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := m.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt — обратная операция.
func (m *Manager) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secrets: base64: %w", err)
	}
	ns := m.aead.NonceSize()
	if len(raw) < ns+1 {
		return "", errors.New("secrets: слишком короткий шифротекст")
	}
	plain, err := m.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("secrets: open: %w", err)
	}
	return string(plain), nil
}
