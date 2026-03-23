package crawler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAmazonSearchDiscoverabilityFirstOrganic(t *testing.T) {
	htmlContent := []byte(`
		<html>
			<body>
				<div data-component-type="s-search-result" data-asin="B00TARGET123"></div>
				<div data-component-type="s-search-result" data-asin="B00OTHER123"></div>
			</body>
		</html>
	`)

	discoverability, err := parseAmazonSearchDiscoverability(
		htmlContent,
		"B00TARGET123",
		"https://www.amazon.com/s?k=B00TARGET123",
	)
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusFirstOrganic, discoverability.Status)
	require.Equal(t, 1, discoverability.TargetOrganicRank)
	require.Equal(t, "B00TARGET123", discoverability.FirstOrganicASIN)
	require.Equal(t, 0, discoverability.SponsoredBeforeTargetCount)
	require.Equal(t, "https://www.amazon.com/s?k=B00TARGET123", discoverability.SearchURL)
}

func TestParseAmazonSearchDiscoverabilityOrganicNotFirst(t *testing.T) {
	htmlContent := []byte(`
		<html>
			<body>
				<div data-component-type="s-search-result" data-asin="B00SPONSORED1">
					<span>Sponsored</span>
				</div>
				<div data-component-type="s-search-result" data-asin="B00OTHER123"></div>
				<div data-component-type="s-search-result" data-asin="B00TARGET123"></div>
			</body>
		</html>
	`)

	discoverability, err := parseAmazonSearchDiscoverability(
		htmlContent,
		"B00TARGET123",
		"https://www.amazon.com/s?k=B00TARGET123",
	)
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusOrganicNotFirst, discoverability.Status)
	require.Equal(t, 2, discoverability.TargetOrganicRank)
	require.Equal(t, "B00OTHER123", discoverability.FirstOrganicASIN)
	require.Equal(t, 1, discoverability.SponsoredBeforeTargetCount)
}

func TestParseAmazonSearchDiscoverabilityNotFound(t *testing.T) {
	htmlContent := []byte(`
		<html>
			<body>
				<div data-component-type="s-search-result" data-asin="B00OTHER123"></div>
				<div data-component-type="s-search-result" data-asin="B00OTHER456"></div>
			</body>
		</html>
	`)

	discoverability, err := parseAmazonSearchDiscoverability(
		htmlContent,
		"B00TARGET123",
		"https://www.amazon.com/s?k=B00TARGET123",
	)
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusNotFound, discoverability.Status)
	require.Equal(t, 0, discoverability.TargetOrganicRank)
	require.Equal(t, "B00OTHER123", discoverability.FirstOrganicASIN)
	require.Equal(t, 0, discoverability.SponsoredBeforeTargetCount)
}

func TestParseAmazonSearchDiscoverabilitySponsoredOnly(t *testing.T) {
	htmlContent := []byte(`
		<html>
			<body>
				<div data-component-type="s-search-result" data-asin="B00SPONSORED1">
					<span>Sponsored</span>
				</div>
				<div data-component-type="s-search-result" data-asin="B00SPONSORED2">
					<span>Sponsored</span>
				</div>
			</body>
		</html>
	`)

	discoverability, err := parseAmazonSearchDiscoverability(
		htmlContent,
		"B00TARGET123",
		"https://www.amazon.com/s?k=B00TARGET123",
	)
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusSponsoredOnly, discoverability.Status)
	require.Equal(t, 0, discoverability.TargetOrganicRank)
	require.Equal(t, "", discoverability.FirstOrganicASIN)
	require.Equal(t, 0, discoverability.SponsoredBeforeTargetCount)
}

func TestParseAmazonSearchDiscoverabilityBlocked(t *testing.T) {
	htmlContent := []byte(`
		<html>
			<body>
				<form action="/errors/validateCaptcha">
					<input id="captchacharacters" />
				</form>
				<div>Type the characters you see in this image:</div>
			</body>
		</html>
	`)

	discoverability, err := parseAmazonSearchDiscoverability(
		htmlContent,
		"B00TARGET123",
		"https://www.amazon.com/s?k=B00TARGET123",
	)
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusBlocked, discoverability.Status)
	require.Equal(t, 0, discoverability.TargetOrganicRank)
	require.Equal(t, "", discoverability.FirstOrganicASIN)
	require.Equal(t, 0, discoverability.SponsoredBeforeTargetCount)
}
