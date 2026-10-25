package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const (
	base62Chars  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	qrCodeLength = 8
)

// GenerateQRCode membuat kode QR nasabah berformat <prefix><8 karakter base62>,
// misal "BSK-7HQ2D9Lm". Menggunakan crypto/rand agar tidak bisa ditebak.
func GenerateQRCode(prefix string) (string, error) {
	max := big.NewInt(int64(len(base62Chars)))
	buf := make([]byte, qrCodeLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("gagal membangkitkan kode QR: %w", err)
		}
		buf[i] = base62Chars[n.Int64()]
	}
	return prefix + string(buf), nil
}
