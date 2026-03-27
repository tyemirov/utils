package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPaddleWebhookVerifierVerifyAcceptsValidSignature(t *testing.T) {
	secret := "ntfset_test_secret"
	payload := []byte(`{"event_id":"evt_123","event_type":"transaction.completed"}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Unix()

	verifier, err := NewPaddleWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, err)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createPaddleSignatureHeader(secret, timestamp, payload)

	verifyErr := verifier.Verify(signatureHeader, payload)
	require.NoError(t, verifyErr)
}

func TestPaddleWebhookVerifierVerifyRejectsExpiredTimestamp(t *testing.T) {
	secret := "ntfset_test_secret"
	payload := []byte(`{"event_id":"evt_123","event_type":"transaction.completed"}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Add(-10 * time.Minute).Unix()

	verifier, err := NewPaddleWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, err)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createPaddleSignatureHeader(secret, timestamp, payload)

	verifyErr := verifier.Verify(signatureHeader, payload)
	require.ErrorIs(t, verifyErr, ErrPaddleWebhookTimestampExpired)
}

func TestPaddleWebhookVerifierVerifyRejectsInvalidSignature(t *testing.T) {
	secret := "ntfset_test_secret"
	payload := []byte(`{"event_id":"evt_123","event_type":"transaction.completed"}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Unix()

	verifier, err := NewPaddleWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, err)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createPaddleSignatureHeader("ntfset_other_secret", timestamp, payload)

	verifyErr := verifier.Verify(signatureHeader, payload)
	require.ErrorIs(t, verifyErr, ErrPaddleWebhookSignatureInvalid)
}

func TestPaddleWebhookVerifierVerifyRejectsInvalidHeader(t *testing.T) {
	secret := "ntfset_test_secret"
	payload := []byte(`{"event_id":"evt_123","event_type":"transaction.completed"}`)

	verifier, err := NewPaddleWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, err)

	verifyErr := verifier.Verify("invalid-header", payload)
	require.ErrorIs(t, verifyErr, ErrPaddleWebhookHeaderInvalid)
}

func createPaddleSignatureHeader(secret string, timestamp int64, payload []byte) string {
	message := strconv.FormatInt(timestamp, 10) + ":" + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))
	return "ts=" + strconv.FormatInt(timestamp, 10) + ";h1=" + signature
}

func TestNewPaddleWebhookVerifierRejectsEmptySecret(t *testing.T) {
	_, err := NewPaddleWebhookVerifier("", 5*time.Minute)
	require.ErrorIs(t, err, ErrPaddleWebhookSecretEmpty)

	_, err = NewPaddleWebhookVerifier("   ", 5*time.Minute)
	require.ErrorIs(t, err, ErrPaddleWebhookSecretEmpty)
}

func TestNewPaddleWebhookVerifierRejectsZeroMaxAge(t *testing.T) {
	_, err := NewPaddleWebhookVerifier("secret", 0)
	require.ErrorIs(t, err, ErrPaddleWebhookMaxAgeInvalid)
}

func TestNewPaddleWebhookVerifierRejectsNegativeMaxAge(t *testing.T) {
	_, err := NewPaddleWebhookVerifier("secret", -1*time.Minute)
	require.ErrorIs(t, err, ErrPaddleWebhookMaxAgeInvalid)
}

func TestParsePaddleSignatureHeaderEmptySegments(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("")
	require.ErrorIs(t, err, ErrPaddleWebhookHeaderInvalid)
}

func TestParsePaddleSignatureHeaderMissingTimestampKey(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("h1=abcdef")
	require.ErrorIs(t, err, ErrPaddleWebhookTimestampInvalid)
}

func TestParsePaddleSignatureHeaderNoEqualsSign(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("noequalssign")
	require.ErrorIs(t, err, ErrPaddleWebhookHeaderInvalid)
}

func TestParsePaddleSignatureHeaderEmptyHashValue(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("ts=1234567890;h1=")
	require.ErrorIs(t, err, ErrPaddleWebhookHeaderInvalid)
}

// Coverage gap tests for paddle_webhook.go

func TestPaddleWebhookVerifyNilVerifier(t *testing.T) {
	var verifier *PaddleWebhookVerifier
	err := verifier.Verify("ts=123;h1=abc", []byte("payload"))
	require.ErrorIs(t, err, ErrPaddleWebhookVerifierUnavailable)
}

func TestPaddleWebhookVerifyFutureTimestamp(t *testing.T) {
	verifier, verifierErr := NewPaddleWebhookVerifier("secret", 5*time.Minute)
	require.NoError(t, verifierErr)
	futureTime := time.Now().Add(10 * time.Minute)
	verifier.now = func() time.Time { return time.Now().Add(-20 * time.Minute) }
	ts := strconv.FormatInt(futureTime.Unix(), 10)
	header := "ts=" + ts + ";h1=invalid"
	err := verifier.Verify(header, []byte("payload"))
	require.ErrorIs(t, err, ErrPaddleWebhookTimestampExpired)
}

func TestParsePaddleSignatureHeaderMissingTimestampOnly(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("h1=abc123")
	require.ErrorIs(t, err, ErrPaddleWebhookTimestampInvalid)
}

func TestParsePaddleSignatureHeaderMissingHashOnly(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("ts=123")
	require.ErrorIs(t, err, ErrPaddleWebhookHeaderInvalid)
}

func TestParsePaddleSignatureHeaderInvalidTimestampValue(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("ts=notanumber;h1=abc")
	require.ErrorIs(t, err, ErrPaddleWebhookTimestampInvalid)
}

func TestParsePaddleSignatureHeaderSkipsEmptySegments(t *testing.T) {
	_, _, err := parsePaddleSignatureHeader("ts=123;;h1=abc")
	require.NoError(t, err)
}
