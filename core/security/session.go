package security

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type SessionClaims struct {
	Version int64 `json:"ver"`
	jwt.RegisteredClaims
}

type Sessions struct {
	key    []byte
	issuer string
}

func NewSessions(key []byte, issuer string) (*Sessions, error) {
	if len(key) != 32 || issuer == "" {
		return nil, errors.New("invalid session configuration")
	}
	return &Sessions{key: append([]byte(nil), key...), issuer: issuer}, nil
}

func (s *Sessions) Issue(userID string, version int64, now time.Time, ttl time.Duration) (string, error) {
	if userID == "" || ttl <= 0 || ttl > 15*24*time.Hour {
		return "", errors.New("invalid session request")
	}
	claims := SessionClaims{Version: version, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: s.issuer, Subject: userID, ID: NewID("sess"),
		IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
}

func (s *Sessions) Parse(token string, now time.Time) (*SessionClaims, error) {
	claims := new(SessionClaims)
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) { return s.key, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.issuer), jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.Version < 0 {
		return nil, errors.New("invalid or expired session")
	}
	return claims, nil
}
