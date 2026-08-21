package dockerenv

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// TOTP computes the six-digit RFC 6238 code for secret at time at: HMAC-SHA1 over a 30-second step.
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

	// Dynamic truncation (RFC 4226 §5.3): the low nibble of the last byte picks the four digest bytes.
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", code)
}

func Code() string { return TOTP(Secret, time.Now()) }
