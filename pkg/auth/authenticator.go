package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

type Authenticator struct {
	secret []byte
}

func NewAuthenticator(secret string) *Authenticator {
	h := sha256.New()
	h.Write([]byte(secret))
	return &Authenticator{secret: h.Sum(nil)}
}

func GenerateChallenge() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *Authenticator) ComputeResponse(challenge string) string {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(challenge))
	return hex.EncodeToString(h.Sum(nil))
}

func (a *Authenticator) ValidateResponse(challenge, response string) bool {
	expected := a.ComputeResponse(challenge)
	return hmac.Equal([]byte(expected), []byte(response))
}
