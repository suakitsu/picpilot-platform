package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Vault struct{ cipher cipher.AEAD }

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{cipher: aead}, nil
}

// Scope binds ciphertext to its owner and record, preventing cross-user key substitution.
func (v *Vault) Encrypt(scope, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, v.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("secure encryption failed")
	}
	ciphertext := v.cipher.Seal(nonce, nonce, []byte(plaintext), []byte(scope))
	return "v1:" + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (v *Vault) Decrypt(scope, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, "v1:") {
		return "", errors.New("unsupported encrypted credential format")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, "v1:"))
	if err != nil || len(data) < v.cipher.NonceSize()+v.cipher.Overhead() {
		return "", errors.New("credential decryption failed")
	}
	plain, err := v.cipher.Open(nil, data[:v.cipher.NonceSize()], data[v.cipher.NonceSize():], []byte(scope))
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	return string(plain), nil
}

func RandomToken(prefix string, length int) (string, error) {
	if length < 24 {
		return "", errors.New("token entropy is too low")
	}
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", errors.New("secure token generation failed")
	}
	return prefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func NewID(prefix string) string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		panic("secure random unavailable")
	}
	return prefix + "_" + hex.EncodeToString(data)
}

func TokenHash(pepper []byte, value string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func ConstantEqual(a, b string) bool {
	left, right := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}

func HashPassword(password string) (string, error) {
	if len([]byte(password)) < 12 || len([]byte(password)) > 72 {
		return "", errors.New("password must contain 12 to 72 UTF-8 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", errors.New("password hashing failed")
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
