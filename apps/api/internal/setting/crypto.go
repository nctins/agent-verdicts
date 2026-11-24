package setting

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const keyFile = "settings.key"

// Cipher seals API keys with AES-256-GCM.
type Cipher struct{ aead cipher.AEAD }

// LoadKey reads dir/settings.key, creating it with 32 random bytes and mode
// 0600 when missing. A present file of the wrong size is an error: replacing
// it would make every stored key unreadable.
func LoadKey(dir string) (*Cipher, error) {
	path := filepath.Join(dir, keyFile)
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err = createKey(dir, path)
	}
	if err != nil {
		return nil, fmt.Errorf("setting: load key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("setting: %s is %d bytes, want 32", path, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// createKey writes a temp file and renames it so a crash never leaves a
// half-written key behind.
func createKey(dir, path string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, keyFile+".tmp*") // CreateTemp uses mode 0600
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return nil, err
	}
	return key, nil
}

// Seal returns base64(nonce|ciphertext).
func (c *Cipher) Seal(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Open reverses Seal. It fails for data sealed under another key.
func (c *Cipher) Open(enc string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(b) < c.aead.NonceSize() {
		return "", errors.New("setting: malformed sealed value")
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, b[:n], b[n:], nil)
	if err != nil {
		return "", errors.New("setting: cannot decrypt sealed value")
	}
	return string(plain), nil
}
