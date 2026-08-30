// Package crypto 提供对称加密工具（AES-256-GCM）。
//
// 用途：敏感配置字段的应用层加密存储（如 ai_agent.api_key）。
// 密钥来源：conf.Security.DataKey（任意长度字符串，经 SHA-256 派生 32 字节 AES 密钥）。
// 密文格式：base64(nonce(12B) || ciphertext+tag)，自包含、无需额外存 nonce。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrEmptyKey 密钥为空（未配置 conf.Security.DataKey）。
var ErrEmptyKey = errors.New("crypto: data key is empty")

// ErrInvalidCiphertext 密文格式非法（不是合法 base64 或短于 nonce+tag）。
var ErrInvalidCiphertext = errors.New("crypto: invalid ciphertext")

// newGCM 用密钥字符串派生 AES-256-GCM。
// 派生方式：SHA-256(key) -> 32 字节，兼容任意长度的配置密钥。
func newGCM(key string) (cipher.AEAD, error) {
	if key == "" {
		return nil, ErrEmptyKey
	}
	k := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// Encrypt 用 AES-256-GCM 加密明文，返回 base64(nonce || ciphertext+tag)。
func Encrypt(key, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: read nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 Encrypt 的产物。密钥不符/密文被篡改时返回错误。
func Decrypt(key, ciphertext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return "", ErrInvalidCiphertext
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: open: %w", err)
	}
	return string(plain), nil
}

// Mask 返回掩码形式（如 "sk-****abcd"），用于敏感字段回显。
// 长度 <= 8 时只返回 "****"，避免短值泄露过多字符。
func Mask(plaintext string) string {
	if len(plaintext) <= 8 {
		return "****"
	}
	return plaintext[:3] + "****" + plaintext[len(plaintext)-4:]
}
