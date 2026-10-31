package engine

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// setKeyLocked installs the 32-byte secret the Android app keeps protected by
// the phone's keystore. The caller must hold s.mu.
func (s *engineState) setKeyLocked(h string) {
	if len(s.key) > 0 {
		return
	}
	if b, err := hex.DecodeString(h); err == nil && len(b) == 32 {
		s.key = b
	}
}

// seal encrypts data with AES-256-GCM. The caller must hold s.mu.
func (s *engineState) seal(plain []byte) ([]byte, error) {
	if len(s.key) == 0 {
		return nil, errors.New("no key")
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// open decrypts data made by seal. The caller must hold s.mu.
func (s *engineState) open(data []byte) ([]byte, error) {
	if len(s.key) == 0 {
		return nil, errors.New("no key")
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("token file is too short")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
