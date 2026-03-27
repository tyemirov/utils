package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatUSDPriceCentsZero(t *testing.T) {
	require.Equal(t, "", formatUSDPriceCents(0))
}

func TestFormatUSDPriceCentsNegative(t *testing.T) {
	require.Equal(t, "", formatUSDPriceCents(-100))
}

func TestFormatUSDPriceCentsWholeNumber(t *testing.T) {
	require.Equal(t, "$10", formatUSDPriceCents(1000))
}

func TestFormatUSDPriceCentsWithCents(t *testing.T) {
	require.Equal(t, "$10.50", formatUSDPriceCents(1050))
}

func TestFormatUSDPriceCentsCentsOnly(t *testing.T) {
	require.Equal(t, "$0.50", formatUSDPriceCents(50))
}
