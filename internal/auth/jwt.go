package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenIssuer = "banksampahku-api"

// ErrInvalidToken dikembalikan jika token tidak valid, kedaluwarsa, atau sudah dicabut
var ErrInvalidToken = errors.New("token tidak valid atau sudah kedaluwarsa")

// Claims adalah payload JWT: sub (user id), role, school_id + registered claims standar
type Claims struct {
	Role     string `json:"role"`
	SchoolID string `json:"school_id,omitempty"`
	jwt.RegisteredClaims
}

// TokenManager menerbitkan dan memverifikasi JWT HS256
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager membuat TokenManager dengan secret & masa berlaku (jam)
func NewTokenManager(secret string, expiredHours int) *TokenManager {
	return &TokenManager{
		secret: []byte(secret),
		ttl:    time.Duration(expiredHours) * time.Hour,
	}
}

// TTL mengembalikan masa berlaku token
func (m *TokenManager) TTL() time.Duration { return m.ttl }

// Generate menerbitkan access token untuk user
func (m *TokenManager) Generate(user *User) (string, time.Time, error) {
	jti, err := randomID()
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	expiresAt := now.Add(m.ttl)

	schoolID := ""
	if user.SchoolID != nil {
		schoolID = *user.SchoolID
	}

	claims := Claims{
		Role:     user.Role,
		SchoolID: schoolID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   user.ID,
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gagal menandatangani token: %w", err)
	}
	return token, expiresAt, nil
}

// Parse memverifikasi signature, algoritma (hanya HS256), issuer, dan masa berlaku token
func (m *TokenManager) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || claims.Subject == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// randomID membuat ID acak 128-bit (hex) untuk claim jti
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gagal membangkitkan token id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
