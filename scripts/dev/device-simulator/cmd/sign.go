// Package main provides HMAC-SHA256 signing utilities for device simulator.
// 对齐：docs/ §4 · docs/ §3.2
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SignDeviceRequest generates HMAC-SHA256 signature for device API requests.
// Signature string format: {method}{path}{timestamp_unix}{body}
func SignDeviceRequest(secret, method, path, body string, ts time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	sigStr := fmt.Sprintf("%s%s%d%s", method, path, ts.Unix(), body)
	mac.Write([]byte(sigStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyDeviceSignature verifies a device HMAC-SHA256 signature.
// Returns true if the signature matches.
func VerifyDeviceSignature(secret, method, path, body string, ts time.Time, expectedSig string) bool {
	actualSig := SignDeviceRequest(secret, method, path, body, ts)
	return hmac.Equal([]byte(actualSig), []byte(expectedSig))
}

// IsTimestampValid checks if the request timestamp is within ±5 minutes of server time.
// This prevents replay attacks by rejecting requests with stale timestamps.
func IsTimestampValid(ts time.Time, serverTime time.Time, windowMinutes int) bool {
	diff := serverTime.Sub(ts)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(windowMinutes)*time.Minute
}

// RFC4231TC2 key/input for self-validation
const rfc4231TC2Key = "Jefe"
const rfc4231TC2Input = "what do ya want for nothing?"
const rfc4231TC2Expected = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
