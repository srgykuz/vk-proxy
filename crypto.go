package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var errInvalidKey = errors.New("key must be 32 bytes")

func hexToKey(s string) ([]byte, error) {
	key, err := hex.DecodeString(s)

	if err != nil {
		return nil, err
	}

	if len(key) != 32 {
		return nil, errInvalidKey
	}

	return key, nil
}

func encrypt(data []byte, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errInvalidKey
	}

	block, err := aes.NewCipher(key)

	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)

	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())

	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	return ciphertext, nil
}

func decrypt(data []byte, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errInvalidKey
	}

	block, err := aes.NewCipher(key)

	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)

	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()

	if len(data) < nonceSize {
		return nil, errors.New("malformed")
	}

	nonce, data := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)

	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
