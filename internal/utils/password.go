package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost sesuai PRD Bab 6A & Bab 8 (bcrypt cost 12)
const BcryptCost = 12

// ErrPasswordMismatch dikembalikan jika password tidak cocok dengan hash
var ErrPasswordMismatch = errors.New("password tidak cocok")

// PasswordHasher melakukan hashing password dengan skema bcrypt + pepper:
//
//	hash = bcrypt( base64( HMAC-SHA256(pepper, password) ), cost 12 )
//
// Salt dibangkitkan acak oleh bcrypt di setiap hash dan tersimpan di dalam string hash.
// Pepper adalah kunci rahasia dari env PASSWORD_PEPPER yang TIDAK disimpan di database,
// sehingga hash yang bocor tidak bisa di-brute-force tanpa pepper. HMAC juga menormalkan
// panjang input menjadi 44 karakter, menghindari batas 72 byte bcrypt.
type PasswordHasher struct {
	pepper []byte
}

// NewPasswordHasher membuat hasher dengan pepper dari konfigurasi.
func NewPasswordHasher(pepper string) *PasswordHasher {
	return &PasswordHasher{pepper: []byte(pepper)}
}

// peppered menghitung HMAC-SHA256(pepper, password) dalam bentuk base64.
func (h *PasswordHasher) peppered(password string) []byte {
	mac := hmac.New(sha256.New, h.pepper)
	mac.Write([]byte(password))
	sum := mac.Sum(nil)

	out := make([]byte, base64.StdEncoding.EncodedLen(len(sum)))
	base64.StdEncoding.Encode(out, sum)
	return out
}

// Hash menghasilkan hash bcrypt + pepper dari password plaintext.
func (h *PasswordHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(h.peppered(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("gagal hash password: %w", err)
	}
	return string(hash), nil
}

// Compare memeriksa kecocokan password plaintext dengan hash yang tersimpan.
func (h *PasswordHasher) Compare(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), h.peppered(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrPasswordMismatch
	}
	return err
}

// tempPasswordChars tanpa karakter yang mudah tertukar (0/O, 1/l/I)
const tempPasswordChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// GenerateTempPassword membuat password sementara acak (crypto/rand) yang selalu
// lolos ValidatePasswordStrength, untuk akun yang dibuat oleh admin.
func GenerateTempPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}
	max := big.NewInt(int64(len(tempPasswordChars)))
	for {
		buf := make([]byte, length)
		for i := range buf {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("gagal membangkitkan password sementara: %w", err)
			}
			buf[i] = tempPasswordChars[n.Int64()]
		}
		if pw := string(buf); ValidatePasswordStrength(pw) == nil {
			return pw, nil
		}
	}
}

// ValidatePasswordStrength memastikan password minimal 8 karakter
// serta mengandung kombinasi huruf dan angka (PRD Bab 6A).
func ValidatePasswordStrength(password string) error {
	if len([]rune(password)) < 8 {
		return errors.New("Password minimal 8 karakter")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("Password harus mengandung kombinasi huruf dan angka")
	}
	return nil
}
