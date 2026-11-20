package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

type jwtClaims struct {
	Exp int64  `json:"exp"`
	Sub string `json:"sub"`
}

func signJWT(secret string, exp int64, sub string) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	p, _ := json.Marshal(jwtClaims{Exp: exp, Sub: sub})
	payload := base64.RawURLEncoding.EncodeToString(p)
	signing := h + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyJWT(secret, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var c jwtClaims
	if json.Unmarshal(payload, &c) != nil {
		return false
	}
	if c.Exp == 0 || c.Exp < time.Now().Unix() {
		return false
	}
	return true
}

// keyAuth requires a valid x-api-key (strict when API_KEYS is configured).
func (s *Server) keyAuth(c fiber.Ctx) error {
	key := c.Get("x-api-key")
	if key == "" {
		return c.Status(401).JSON(fiber.Map{"message": "API key required"})
	}
	if len(s.cfg.APIKeys) > 0 {
		ok := false
		for _, k := range s.cfg.APIKeys {
			if hmac.Equal([]byte(k), []byte(key)) {
				ok = true
				break
			}
		}
		if !ok {
			return c.Status(401).JSON(fiber.Map{"message": "Invalid API key"})
		}
	}
	return c.Next()
}

// bearerAuth requires a valid JWT issued by /v1/auth (signature + exp checked).
func (s *Server) bearerAuth(c fiber.Ctx) error {
	auth := c.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return c.Status(401).JSON(fiber.Map{"message": "Authorization header is empty (Bearer JWTtoken is required)"})
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if !verifyJWT(s.cfg.JWTSecret, token) {
		return c.Status(401).JSON(fiber.Map{"message": "Invalid token"})
	}
	return c.Next()
}
