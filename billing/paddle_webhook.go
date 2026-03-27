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
	paddleSignatureTimestampKey = "ts"
	paddleSignatureHashKey      = "h1"
)

var (
	ErrPaddleWebhookVerifierUnavailable = errors.New("billing.paddle.webhook.verifier.unavailable")
	ErrPaddleWebhookSecretEmpty         = errors.New("billing.paddle.webhook.secret.empty")
	ErrPaddleWebhookMaxAgeInvalid       = errors.New("billing.paddle.webhook.max_age.invalid")
	ErrPaddleWebhookHeaderInvalid       = errors.New("billing.paddle.webhook.signature_header.invalid")
	ErrPaddleWebhookTimestampInvalid    = errors.New("billing.paddle.webhook.timestamp.invalid")
	ErrPaddleWebhookTimestampExpired    = errors.New("billing.paddle.webhook.timestamp.expired")
	ErrPaddleWebhookSignatureInvalid    = errors.New("billing.paddle.webhook.signature.invalid")
)

// PaddleWebhookEnvelope represents the top-level structure of Paddle webhook notifications.
type PaddleWebhookEnvelope struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"`
}

// PaddleWebhookVerifier verifies Paddle webhook signatures.
type PaddleWebhookVerifier struct {
	webhookSecret string
	maxAge        time.Duration
	now           func() time.Time
}

// NewPaddleWebhookVerifier constructs a verifier with a webhook secret and timestamp tolerance.
func NewPaddleWebhookVerifier(webhookSecret string, maxAge time.Duration) (*PaddleWebhookVerifier, error) {
	normalizedWebhookSecret := strings.TrimSpace(webhookSecret)
	if normalizedWebhookSecret == "" {
		return nil, ErrPaddleWebhookSecretEmpty
	}
	if maxAge <= 0 {
		return nil, ErrPaddleWebhookMaxAgeInvalid
	}
	return &PaddleWebhookVerifier{
		webhookSecret: normalizedWebhookSecret,
		maxAge:        maxAge,
		now:           time.Now,
	}, nil
}

// Verify validates Paddle-Signature against payload bytes.
func (verifier *PaddleWebhookVerifier) Verify(signatureHeader string, payload []byte) error {
	if verifier == nil {
		return ErrPaddleWebhookVerifierUnavailable
	}
	timestamp, signatures, err := parsePaddleSignatureHeader(signatureHeader)
	if err != nil {
		return err
	}

	eventTime := time.Unix(timestamp, 0).UTC()
	currentTime := verifier.now().UTC()
	age := currentTime.Sub(eventTime)
	if age < 0 {
		age = -age
	}
	if age > verifier.maxAge {
		return ErrPaddleWebhookTimestampExpired
	}

	signedPayload := strconv.FormatInt(timestamp, 10) + ":" + string(payload)
	mac := hmac.New(sha256.New, []byte(verifier.webhookSecret))
	_, _ = mac.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	expectedSignatureBytes := []byte(expectedSignature)

	for _, signature := range signatures {
		signatureBytes := []byte(strings.ToLower(signature))
		if subtle.ConstantTimeCompare(expectedSignatureBytes, signatureBytes) == 1 {
			return nil
		}
	}

	return ErrPaddleWebhookSignatureInvalid
}

func parsePaddleSignatureHeader(signatureHeader string) (int64, []string, error) {
	normalizedHeader := strings.TrimSpace(signatureHeader)
	if normalizedHeader == "" {
		return 0, nil, ErrPaddleWebhookHeaderInvalid
	}

	segments := strings.Split(normalizedHeader, ";")
	signatures := make([]string, 0, 1)

	var timestamp int64
	timestampFound := false

	for _, segment := range segments {
		normalizedSegment := strings.TrimSpace(segment)
		if normalizedSegment == "" {
			continue
		}

		key, value, ok := strings.Cut(normalizedSegment, "=")
		if !ok {
			return 0, nil, ErrPaddleWebhookHeaderInvalid
		}

		normalizedKey := strings.TrimSpace(key)
		normalizedValue := strings.TrimSpace(value)

		switch normalizedKey {
		case paddleSignatureTimestampKey:
			parsedTimestamp, parseErr := strconv.ParseInt(normalizedValue, 10, 64)
			if parseErr != nil {
				return 0, nil, ErrPaddleWebhookTimestampInvalid
			}
			timestamp = parsedTimestamp
			timestampFound = true
		case paddleSignatureHashKey:
			if normalizedValue == "" {
				return 0, nil, ErrPaddleWebhookHeaderInvalid
			}
			signatures = append(signatures, normalizedValue)
		}
	}

	if !timestampFound {
		return 0, nil, ErrPaddleWebhookTimestampInvalid
	}
	if len(signatures) == 0 {
		return 0, nil, ErrPaddleWebhookHeaderInvalid
	}

	return timestamp, signatures, nil
}
