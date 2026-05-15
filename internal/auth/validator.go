package auth

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const clockSkewLeeway = 30 * time.Second

type Validator struct {
	keyStore *KeyStore
	issuer   string
	audience string
}

func NewValidator(keyStore *KeyStore, issuer, audience string) *Validator {
	return &Validator{
		keyStore: keyStore,
		issuer:   issuer,
		audience: audience,
	}
}

func (v *Validator) Validate(tokenString string) error {
	parser := jwt.NewParser(
		jwt.WithLeeway(clockSkewLeeway),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	)

	token, err := parser.Parse(tokenString, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid in token header")
		}
		return v.keyStore.GetKey(kid)
	})
	if err != nil {
		v.logAuthFailure(tokenString, err)
		return fmt.Errorf("invalid token: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("unexpected claims type")
	}
	if _, hasIat := claims["iat"]; !hasIat {
		return fmt.Errorf("missing required iat claim")
	}

	return nil
}

func (v *Validator) logAuthFailure(tokenStr string, err error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, _ := parser.ParseUnverified(tokenStr, jwt.MapClaims{})

	kid := "unknown"
	hashedSub := "unknown"

	if token != nil {
		if k, ok := token.Header["kid"].(string); ok {
			kid = k
		}
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if sub, ok := claims["sub"].(string); ok {
				hash := sha256.Sum256([]byte(sub))
				hashedSub = fmt.Sprintf("%x", hash[:4])
			}
		}
	}

	slog.Debug("auth validation failed",
		"reason", err.Error(),
		"kid", kid,
		"sub_hash", hashedSub,
	)
}
