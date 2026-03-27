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

func TestStripeWebhookVerifierVerifyAcceptsValidSignature(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123","type":"checkout.session.completed","created":1750000000}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Unix()

	verifier, verifierErr := NewStripeWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, verifierErr)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createStripeSignatureHeader(secret, timestamp, payload)
	verifyErr := verifier.Verify(signatureHeader, payload)
	require.NoError(t, verifyErr)
}

func TestStripeWebhookVerifierVerifyRejectsExpiredTimestamp(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123","type":"checkout.session.completed","created":1750000000}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Add(-10 * time.Minute).Unix()

	verifier, verifierErr := NewStripeWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, verifierErr)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createStripeSignatureHeader(secret, timestamp, payload)
	verifyErr := verifier.Verify(signatureHeader, payload)
	require.ErrorIs(t, verifyErr, ErrStripeWebhookTimestampExpired)
}

func TestStripeWebhookVerifierVerifyRejectsInvalidSignature(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_123","type":"checkout.session.completed","created":1750000000}`)
	now := time.Unix(1_750_000_000, 0).UTC()
	timestamp := now.Unix()

	verifier, verifierErr := NewStripeWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, verifierErr)
	verifier.now = func() time.Time {
		return now
	}

	signatureHeader := createStripeSignatureHeader("whsec_other_secret", timestamp, payload)
	verifyErr := verifier.Verify(signatureHeader, payload)
	require.ErrorIs(t, verifyErr, ErrStripeWebhookSignatureInvalid)
}

func createStripeSignatureHeader(secret string, timestamp int64, payload []byte) string {
	message := strconv.FormatInt(timestamp, 10) + "." + string(payload)
	signatureMAC := hmac.New(sha256.New, []byte(secret))
	_, _ = signatureMAC.Write([]byte(message))
	signature := hex.EncodeToString(signatureMAC.Sum(nil))
	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + signature
}

func TestNewStripeWebhookVerifierRejectsEmptySecret(t *testing.T) {
	_, err := NewStripeWebhookVerifier("", 5*time.Minute)
	require.ErrorIs(t, err, ErrStripeWebhookSecretEmpty)

	_, err = NewStripeWebhookVerifier("   ", 5*time.Minute)
	require.ErrorIs(t, err, ErrStripeWebhookSecretEmpty)
}

func TestNewStripeWebhookVerifierRejectsZeroMaxAge(t *testing.T) {
	_, err := NewStripeWebhookVerifier("secret", 0)
	require.ErrorIs(t, err, ErrStripeWebhookMaxAgeInvalid)
}

func TestNewStripeWebhookVerifierRejectsNegativeMaxAge(t *testing.T) {
	_, err := NewStripeWebhookVerifier("secret", -1*time.Minute)
	require.ErrorIs(t, err, ErrStripeWebhookMaxAgeInvalid)
}

func TestParseStripeSignatureHeaderMissingTimestamp(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("v1=abcdef")
	require.ErrorIs(t, err, ErrStripeWebhookTimestampInvalid)
}

func TestParseStripeSignatureHeaderEmptyValue(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("")
	require.ErrorIs(t, err, ErrStripeWebhookHeaderInvalid)
}

func TestParseStripeSignatureHeaderNoEqualsSign(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("invalid-segment")
	require.ErrorIs(t, err, ErrStripeWebhookHeaderInvalid)
}

func TestParseStripeSignatureHeaderMissingV1Signature(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("t=1234567890")
	require.ErrorIs(t, err, ErrStripeWebhookHeaderInvalid)
}

func TestParseStripeSignatureHeaderEmptyV1Value(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("t=1234567890,v1=")
	require.ErrorIs(t, err, ErrStripeWebhookHeaderInvalid)
}

// Coverage gap tests for stripe_webhook.go

func TestStripeWebhookVerifyNilVerifier(t *testing.T) {
	var verifier *StripeWebhookVerifier
	err := verifier.Verify("t=123,v1=abc", []byte("payload"))
	require.ErrorIs(t, err, ErrStripeWebhookVerifierUnavailable)
}

func TestStripeWebhookVerifyFutureTimestamp(t *testing.T) {
	verifier, verifierErr := NewStripeWebhookVerifier("secret", 5*time.Minute)
	require.NoError(t, verifierErr)
	verifier.now = func() time.Time { return time.Now().Add(-20 * time.Minute) }
	futureTime := time.Now().Add(10 * time.Minute)
	ts := strconv.FormatInt(futureTime.Unix(), 10)
	header := "t=" + ts + ",v1=invalid"
	err := verifier.Verify(header, []byte("payload"))
	require.ErrorIs(t, err, ErrStripeWebhookTimestampExpired)
}

func TestParseStripeSignatureHeaderInvalidTimestampValue(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("t=notanumber,v1=abc")
	require.ErrorIs(t, err, ErrStripeWebhookTimestampInvalid)
}

func TestParseStripeSignatureHeaderSkipsEmptySegments(t *testing.T) {
	_, _, err := parseStripeSignatureHeader("t=123,,v1=abc")
	require.NoError(t, err)
}

func TestStripeWebhookVerifyParseHeaderError(t *testing.T) {
	verifier, verifierErr := NewStripeWebhookVerifier("secret", 5*time.Minute)
	require.NoError(t, verifierErr)
	err := verifier.Verify("invalid-header", []byte("payload"))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeWebhookHeaderInvalid)
}

func TestStripeWebhookVerifyFutureTimestampWithinTolerance(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_future"}`)
	futureTime := time.Now().Add(2 * time.Minute)
	verifier, verifierErr := NewStripeWebhookVerifier(secret, 5*time.Minute)
	require.NoError(t, verifierErr)
	verifier.now = func() time.Time { return time.Now() }
	header := createStripeSignatureHeader(secret, futureTime.Unix(), payload)
	err := verifier.Verify(header, payload)
	require.NoError(t, err)
}
