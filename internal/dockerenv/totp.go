package dockerenv

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// TOTP computes the six-digit code an authenticator app would be showing for
// secret at time at — RFC 6238 with the defaults pam_google_authenticator uses:
// HMAC-SHA1 over a 30-second step.
//
// It is what lets a test log in the way a human with a phone does. There is no
// dependency for this on purpose: twenty lines of standard library beats another
// module in go.mod for something only the tests touch.
func TOTP(secret string, at time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return ""
	}

	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30))

	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3): the low nibble of the last byte picks
	// which four bytes of the digest the code comes from.
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", code)
}

// Code is the code valid right now.
func Code() string { return TOTP(Secret, time.Now()) }
