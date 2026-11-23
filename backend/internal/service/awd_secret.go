package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// AWD SSH 密码加密（AES-GCM）：密钥由应用 secret 派生。数据库只存密文，
// 参赛者视图按需解密展示；轮换密码 = 重新部署攻击盒。

func awdSecretKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func encryptAwdSecret(secret, plaintext string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("awd secret is empty")
	}
	block, err := aes.NewCipher(awdSecretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func decryptAwdSecret(secret, encoded string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("awd secret is empty")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(awdSecretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func randomAwdPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
