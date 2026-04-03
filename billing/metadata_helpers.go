package billing

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	paddleLegacyMetadataUserEmailKey    = "product_scanner_user_email"
	paddleLegacyMetadataPurchaseKindKey = "product_scanner_purchase_kind"
	paddleLegacyMetadataPlanCodeKey     = "product_scanner_plan_code"
	paddleLegacyMetadataPackCodeKey     = "product_scanner_pack_code"
	paddleLegacyMetadataPackCreditsKey  = "product_scanner_pack_credits"

	stripeLegacyMetadataUserEmailKey    = "poodle_scanner_user_email"
	stripeLegacyMetadataPurchaseKindKey = "poodle_scanner_purchase_kind"
	stripeLegacyMetadataPlanCodeKey     = "poodle_scanner_plan_code"
	stripeLegacyMetadataPackCodeKey     = "poodle_scanner_pack_code"
	stripeLegacyMetadataPackCreditsKey  = "poodle_scanner_pack_credits"
	stripeLegacyMetadataPriceIDKey      = "poodle_scanner_price_id"

	crosswordLegacyMetadataSubjectIDKey = "crossword_user_id"
	crosswordLegacyMetadataUserEmailKey = "user_email"
	crosswordLegacyMetadataPackCodeKey  = "pack_code"
	crosswordLegacyMetadataCreditsKey   = "credits"

	// Deprecated aliases kept for backward compatibility inside the package.
	paddleMetadataUserEmailKey    = paddleLegacyMetadataUserEmailKey
	paddleMetadataPurchaseKindKey = paddleLegacyMetadataPurchaseKindKey
	paddleMetadataPlanCodeKey     = paddleLegacyMetadataPlanCodeKey
	paddleMetadataPackCodeKey     = paddleLegacyMetadataPackCodeKey
	paddleMetadataPackCreditsKey  = paddleLegacyMetadataPackCreditsKey

	stripeMetadataUserEmailKey    = stripeLegacyMetadataUserEmailKey
	stripeMetadataPurchaseKindKey = stripeLegacyMetadataPurchaseKindKey
	stripeMetadataPlanCodeKey     = stripeLegacyMetadataPlanCodeKey
	stripeMetadataPackCodeKey     = stripeLegacyMetadataPackCodeKey
	stripeMetadataPackCreditsKey  = stripeLegacyMetadataPackCreditsKey
	stripeMetadataPriceIDKey      = stripeLegacyMetadataPriceIDKey
)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			return trimmedValue
		}
	}
	return ""
}

func metadataValue(metadata map[string]string, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		value := strings.TrimSpace(metadata[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func webhookMetadataValueAny(metadata map[string]interface{}, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		value, ok := metadata[key]
		if !ok || value == nil {
			continue
		}
		switch typedValue := value.(type) {
		case string:
			if trimmedValue := strings.TrimSpace(typedValue); trimmedValue != "" {
				return trimmedValue
			}
		case float64:
			if typedValue == float64(int64(typedValue)) {
				return strconv.FormatInt(int64(typedValue), 10)
			}
			return strconv.FormatFloat(typedValue, 'f', -1, 64)
		default:
			if renderedValue := strings.TrimSpace(fmt.Sprintf("%v", typedValue)); renderedValue != "" {
				return renderedValue
			}
		}
	}
	return ""
}

func webhookMetadataValue(metadata map[string]interface{}, key string) string {
	return webhookMetadataValueAny(metadata, key)
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	target := make(map[string]string, len(source))
	for rawKey, rawValue := range source {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		target[key] = strings.TrimSpace(rawValue)
	}
	return target
}

func buildCheckoutMetadata(customer CustomerContext, purchaseKind string, itemCode string, credits int64, priceID string) map[string]string {
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedPurchaseKind := NormalizePurchaseKind(purchaseKind)
	resolvedItemCode := strings.TrimSpace(itemCode)
	resolvedPriceID := strings.TrimSpace(priceID)

	metadata := map[string]string{
		billingMetadataUserEmailKey:         normalizedCustomer.Email,
		paddleLegacyMetadataUserEmailKey:    normalizedCustomer.Email,
		stripeLegacyMetadataUserEmailKey:    normalizedCustomer.Email,
		crosswordLegacyMetadataUserEmailKey: normalizedCustomer.Email,
		billingMetadataSubjectIDKey:         normalizedCustomer.SubjectID,
		crosswordLegacyMetadataSubjectIDKey: normalizedCustomer.SubjectID,
		billingMetadataPurchaseKindKey:      normalizedPurchaseKind,
		paddleLegacyMetadataPurchaseKindKey: normalizedPurchaseKind,
		stripeLegacyMetadataPurchaseKindKey: normalizedPurchaseKind,
		billingMetadataPriceIDKey:           resolvedPriceID,
		stripeLegacyMetadataPriceIDKey:      resolvedPriceID,
	}
	switch normalizedPurchaseKind {
	case PurchaseKindSubscription:
		metadata[billingMetadataPlanCodeKey] = resolvedItemCode
		metadata[paddleLegacyMetadataPlanCodeKey] = resolvedItemCode
		metadata[stripeLegacyMetadataPlanCodeKey] = resolvedItemCode
	case PurchaseKindTopUpPack:
		metadata[billingMetadataPackCodeKey] = resolvedItemCode
		metadata[paddleLegacyMetadataPackCodeKey] = resolvedItemCode
		metadata[stripeLegacyMetadataPackCodeKey] = resolvedItemCode
		metadata[crosswordLegacyMetadataPackCodeKey] = resolvedItemCode
		if credits > 0 {
			formattedCredits := strconv.FormatInt(credits, 10)
			metadata[billingMetadataPackCreditsKey] = formattedCredits
			metadata[paddleLegacyMetadataPackCreditsKey] = formattedCredits
			metadata[stripeLegacyMetadataPackCreditsKey] = formattedCredits
			metadata[crosswordLegacyMetadataCreditsKey] = formattedCredits
		}
	}
	for key, value := range cloneStringMap(metadata) {
		if value == "" {
			delete(metadata, key)
		}
	}
	return metadata
}
