package shieldz

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// DefaultTolerance is how far a webhook timestamp may drift from now.
const DefaultTolerance = 300 * time.Second

// Webhook signature errors.
var (
	ErrMissingSignature   = errors.New("shieldz: missing signature header")
	ErrMissingSecret      = errors.New("shieldz: missing signing secret")
	ErrMalformedSignature = errors.New("shieldz: malformed signature header")
	ErrTimestampTolerance = errors.New("shieldz: timestamp outside tolerance")
	ErrNoMatchingSig      = errors.New("shieldz: no matching signature")
)

// VerifySignature checks the X-Shieldz-Signature header against the RAW request body.
// The header is "t=<unix>,v1=<hex>[,v1=<hex>]" (several v1 during secret rotation);
// the signed payload is "<t>.<body>", HMAC-SHA256, hex.
func VerifySignature(rawBody []byte, header, secret string, tolerance time.Duration, now time.Time) error {
	if header == "" {
		return ErrMissingSignature
	}
	if secret == "" {
		return ErrMissingSecret
	}
	var t string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "t="):
			t = part[2:]
		case strings.HasPrefix(part, "v1="):
			sigs = append(sigs, part[3:])
		}
	}
	if t == "" || len(sigs) == 0 {
		return ErrMalformedSignature
	}
	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return ErrMalformedSignature
	}
	if d := now.Unix() - ts; d > int64(tolerance.Seconds()) || -d > int64(tolerance.Seconds()) {
		return ErrTimestampTolerance
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(t + "."))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(expected)) {
			return nil
		}
	}
	return ErrNoMatchingSig
}

// ConstructEvent verifies the signature and decodes the event.
func ConstructEvent(rawBody []byte, header, secret string) (Object, error) {
	if err := VerifySignature(rawBody, header, secret, DefaultTolerance, time.Now()); err != nil {
		return nil, err
	}
	var ev Object
	return ev, json.Unmarshal(rawBody, &ev)
}
