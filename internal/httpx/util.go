package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"runtime"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var errPanic = errors.New("panic recovered")

const codesError = codes.Error

func attrInt(k string, v int) attribute.KeyValue { return attribute.Int(k, v) }

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// randomHex returns n cryptographically random bytes, hex encoded.
// Panics on failure: a system whose CSPRNG is broken must not keep serving.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// stackTrace captures the stack for panic logging, bounded so a deep recursion
// panic cannot emit a megabyte log line.
func stackTrace() string {
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
