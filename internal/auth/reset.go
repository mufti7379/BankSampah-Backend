package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/mailer"
)

// Alasan pengiriman link reset (kolom password_reset_tokens.reason)
const (
	ResetReasonRequested = "requested" // user lupa password (akun tidak terkunci)
	ResetReasonLocked    = "locked"    // user meminta reset saat akun terkunci 5x salah password
)

// resetCooldown mencegah email reset dikirim berulang ke user yang sama
const resetCooldown = 2 * time.Minute

// ErrInvalidResetToken dikembalikan jika token reset salah, kedaluwarsa, atau sudah dipakai
var ErrInvalidResetToken = errors.New("link reset password tidak valid atau sudah kedaluwarsa")

// ForgotPassword membuat token reset & mengirim email ke user (jika email terdaftar).
// Dipanggil saat user mengetik emailnya sendiri di halaman "Lupa Password" -
// termasuk ketika akun terkunci karena 5x salah password.
// Selalu sukses dari sisi client agar keberadaan email tidak bisa ditebak.
func (s *Service) ForgotPassword(ctx context.Context, email, ip string) error {
	email = normalizeEmail(email)
	user, err := s.Repo.FindByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		log.Info().Str("email", maskEmail(email)).Msg("Lupa password: email tidak terdaftar, diabaikan")
		return nil
	}
	if err != nil {
		return err
	}

	// Jika akun sedang terkunci, email menjelaskan bahwa reset juga membuka kunci akun
	reason := ResetReasonRequested
	if s.Limiter.LockedFor(email) > 0 {
		reason = ResetReasonLocked
	}
	return s.issueResetLink(ctx, user, reason, ip)
}

// VerifyResetToken memeriksa token reset (dipakai halaman reset sebelum menampilkan form)
func (s *Service) VerifyResetToken(ctx context.Context, token string) (*User, error) {
	user, err := s.Repo.FindUserByValidResetToken(ctx, hashResetToken(token))
	if errors.Is(err, ErrUserNotFound) {
		return nil, ErrInvalidResetToken
	}
	return user, err
}

// ResetPassword mengganti password memakai token reset, lalu membuka kunci login akun
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	hash, err := s.Hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	user, err := s.Repo.ResetPassword(ctx, hashResetToken(token), hash)
	if errors.Is(err, ErrUserNotFound) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}

	// Password sudah diganti oleh pemilik email: buka kunci login
	s.Limiter.Reset(user.Email)
	log.Info().Str("user_id", user.ID).Msg("Password berhasil direset, kunci login dibuka")
	return nil
}

// issueResetLink membuat token, menyimpannya (hash), lalu mengirim email reset
func (s *Service) issueResetLink(ctx context.Context, user *User, reason, ip string) error {
	if user.Status == StatusInactive {
		log.Info().Str("user_id", user.ID).Msg("Reset password diabaikan: akun nonaktif")
		return nil
	}

	recent, err := s.Repo.HasRecentResetToken(ctx, user.ID, time.Now().Add(-resetCooldown))
	if err != nil {
		return err
	}
	if recent {
		log.Info().Str("user_id", user.ID).Msg("Reset password diabaikan: masih dalam masa cooldown")
		return nil
	}

	token, err := newResetToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(s.PasswordResetTTL)
	if err := s.Repo.CreateResetToken(ctx, user.ID, hashResetToken(token), reason, ip, expiresAt); err != nil {
		return fmt.Errorf("gagal menyimpan token reset: %w", err)
	}

	resetURL, err := buildResetURL(s.PasswordResetURL, token)
	if err != nil {
		return err
	}

	msg, err := mailer.PasswordResetEmail(user.Email, mailer.PasswordResetData{
		FullName:      user.FullName,
		ResetURL:      resetURL,
		ExpireMinutes: int(s.PasswordResetTTL.Minutes()),
		Locked:        reason == ResetReasonLocked,
	})
	if err != nil {
		return err
	}
	if err := s.Mailer.Send(ctx, msg); err != nil {
		return fmt.Errorf("gagal mengirim email reset: %w", err)
	}

	log.Info().Str("user_id", user.ID).Str("reason", reason).Msg("Email reset password dikirim")
	return nil
}

// newResetToken membuat token acak 256-bit (base64url) yang dikirim ke email
func newResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gagal membangkitkan token reset: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashResetToken menghitung SHA-256 token; hanya hash ini yang disimpan di database
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// buildResetURL menambahkan ?token=... ke URL halaman reset frontend
func buildResetURL(base, token string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("PASSWORD_RESET_URL tidak valid: %w", err)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
