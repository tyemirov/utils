package crawler

import "github.com/gocolly/colly/v2"

// GetProductIDFromContext retrieves the product ID from a Colly response context.
func GetProductIDFromContext(resp *colly.Response) string {
	productID := resp.Ctx.Get(ctxProductIDKey)
	if productID == "" {
		return unknownProductID
	}
	return productID
}

func getProductIDFromContext(resp *colly.Response) string {
	return GetProductIDFromContext(resp)
}

// GetContextValue returns a value from a Colly context, or the fallback if absent.
func GetContextValue(ctx *colly.Context, key, fallback string) string {
	if ctx == nil {
		return fallback
	}
	value := ctx.Get(key)
	if value == "" {
		return fallback
	}
	return value
}

// GetContextInt returns an integer value from a Colly context.
func GetContextInt(ctx *colly.Context, key string) int {
	if ctx == nil {
		return 0
	}
	switch value := ctx.GetAny(key).(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
