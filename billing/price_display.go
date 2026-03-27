package billing

import "fmt"

func formatUSDPriceCents(priceCents int64) string {
	if priceCents <= 0 {
		return ""
	}
	dollars := priceCents / 100
	cents := priceCents % 100
	if cents == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	return fmt.Sprintf("$%d.%02d", dollars, cents)
}
