package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	stripeSignatureTimestampKey = "t"
	stripeSignatureV1HashKey    = "v1"
)

var (
	ErrStripeWebhookVerifierUnavailable = errors.New("billing.stripe.webhook.verifier.unavailable")
	ErrStripeWebhookSecretEmpty         = errors.New("billing.stripe.webhook.secret.empty")
	ErrStripeWebhookMaxAgeInvalid       = errors.New("billing.stripe.webhook.max_age.invalid")
	ErrStripeWebhookHeaderInvalid       = errors.New("billing.stripe.webhook.signature_header.invalid")
	ErrStripeWebhookTimestampInvalid    = errors.New("billing.stripe.webhook.timestamp.invalid")
	ErrStripeWebhookTimestampExpired    = errors.New("billing.stripe.webhook.timestamp.expired")
	ErrStripeWebhookSignatureInvalid    = errors.New("billing.stripe.webhook.signature.invalid")
)

type StripeWebhookVerifier struct {
	webhookSecret string
	maxAge        time.Duration
	now           func() time.Time
}

func NewStripeWebhookVerifier(webhookSecret string, maxAge time.Duration) (*StripeWebhookVerifier, error) {
	normalizedWebhookSecret := strings.TrimSpace(webhookSecret)
	if normalizedWebhookSecret == "" {
		return nil, ErrStripeWebhookSecretEmpty
	}
	if maxAge <= 0 {
		return nil, ErrStripeWebhookMaxAgeInvalid
	}
	return &StripeWebhookVerifier{
		webhookSecret: normalizedWebhookSecret,
		maxAge:        maxAge,
		now:           time.Now,
	}, nil
}

func (verifier *StripeWebhookVerifier) Verify(signatureHeader string, payload []byte) error {
	if verifier == nil {
		return ErrStripeWebhookVerifierUnavailable
	}
	timestamp, signatures, parseErr := parseStripeSignatureHeader(signatureHeader)
	if parseErr != nil {
		return parseErr
	}

	eventTime := time.Unix(timestamp, 0).UTC()
	currentTime := verifier.now().UTC()
	age := currentTime.Sub(eventTime)
	if age < 0 {
		age = -age
	}
	if age > verifier.maxAge {
		return ErrStripeWebhookTimestampExpired
	}

	signedPayload := strconv.FormatInt(timestamp, 10) + "." + string(payload)
	signatureMAC := hmac.New(sha256.New, []byte(verifier.webhookSecret))
	_, _ = signatureMAC.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(signatureMAC.Sum(nil))
	expectedSignatureBytes := []byte(expectedSignature)

	for _, signature := range signatures {
		signatureBytes := []byte(strings.ToLower(signature))
		if subtle.ConstantTimeCompare(expectedSignatureBytes, signatureBytes) == 1 {
			return nil
		}
	}
	return ErrStripeWebhookSignatureInvalid
}

func parseStripeSignatureHeader(signatureHeader string) (int64, []string, error) {
	normalizedHeader := strings.TrimSpace(signatureHeader)
	if normalizedHeader == "" {
		return 0, nil, ErrStripeWebhookHeaderInvalid
	}

	segments := strings.Split(normalizedHeader, ",")
	signatures := make([]string, 0, 1)
	var timestamp int64
	timestampFound := false

	for _, segment := range segments {
		normalizedSegment := strings.TrimSpace(segment)
		if normalizedSegment == "" {
			continue
		}
		key, value, hasKeyValue := strings.Cut(normalizedSegment, "=")
		if !hasKeyValue {
			return 0, nil, ErrStripeWebhookHeaderInvalid
		}
		normalizedKey := strings.TrimSpace(key)
		normalizedValue := strings.TrimSpace(value)
		switch normalizedKey {
		case stripeSignatureTimestampKey:
			parsedTimestamp, parseErr := strconv.ParseInt(normalizedValue, 10, 64)
			if parseErr != nil {
				return 0, nil, ErrStripeWebhookTimestampInvalid
			}
			timestamp = parsedTimestamp
			timestampFound = true
		case stripeSignatureV1HashKey:
			if normalizedValue == "" {
				return 0, nil, ErrStripeWebhookHeaderInvalid
			}
			signatures = append(signatures, normalizedValue)
		}
	}
	if !timestampFound {
		return 0, nil, ErrStripeWebhookTimestampInvalid
	}
	if len(signatures) == 0 {
		return 0, nil, ErrStripeWebhookHeaderInvalid
	}
	return timestamp, signatures, nil
}
