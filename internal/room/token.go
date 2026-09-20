// Package room implements the correspondence-play room store and auth tokens.
package room

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims are embedded in a seat token. Exp == 0 means the token never expires
// (correspondence play).
type Claims struct {
	Room string `json:"room"`
	Seat int    `json:"seat"`
	Exp  int64  `json:"exp,omitempty"`
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// IssueToken creates a signed HS256 JWT for a room seat.
func IssueToken(secret []byte, roomID string, seat int, ttl time.Duration) string {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := Claims{Room: roomID, Seat: seat}
	if ttl > 0 {
		claims.Exp = time.Now().Add(ttl).Unix()
	}
	pb, _ := json.Marshal(claims)
	payload := b64(pb)
	signing := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + b64(mac.Sum(nil))
}

// VerifyToken checks a seat token's signature and expiry.
func VerifyToken(secret []byte, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("malformed token")
	}
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	if !hmac.Equal([]byte(b64(mac.Sum(nil))), []byte(parts[2])) {
		return Claims{}, errors.New("bad signature")
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(pb, &c); err != nil {
		return Claims{}, err
	}
	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return Claims{}, errors.New("token expired")
	}
	if c.Room == "" {
		return Claims{}, errors.New("missing room")
	}
	return c, nil
}

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))[:2*n]
	}
	return hex.EncodeToString(b)
}
