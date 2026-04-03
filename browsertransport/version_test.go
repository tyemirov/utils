package browsertransport

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectChromeVersionLinuxCandidates(t *testing.T) {
	originalGOOS := detectChromeVersionGOOS
	originalOutput := chromeVersionCommandOutput
	t.Cleanup(func() {
		detectChromeVersionGOOS = originalGOOS
		chromeVersionCommandOutput = originalOutput
	})

	detectChromeVersionGOOS = "linux"
	var candidates []string
	chromeVersionCommandOutput = func(candidate string) ([]byte, error) {
		candidates = append(candidates, candidate)
		switch candidate {
		case "google-chrome":
			return nil, errors.New("missing")
		case "google-chrome-stable":
			return []byte("Chromium"), nil
		case "chromium-browser":
			return []byte("Google Chrome 124.0.6367.91"), nil
		default:
			return nil, fmt.Errorf("unexpected candidate %q", candidate)
		}
	}

	require.Equal(t, "124", DetectChromeVersion(""))
	require.Equal(t, []string{"google-chrome", "google-chrome-stable", "chromium-browser"}, candidates)
}

func TestDetectChromeVersionDarwinCandidateAndDefaultUserAgent(t *testing.T) {
	originalGOOS := detectChromeVersionGOOS
	originalOutput := chromeVersionCommandOutput
	t.Cleanup(func() {
		detectChromeVersionGOOS = originalGOOS
		chromeVersionCommandOutput = originalOutput
	})

	detectChromeVersionGOOS = "darwin"
	chromeVersionCommandOutput = func(candidate string) ([]byte, error) {
		require.Equal(t, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", candidate)
		return []byte("Google Chrome 123.0.6312.87"), nil
	}

	require.Equal(t, "123", DetectChromeVersion(""))
	require.Contains(t, DefaultUserAgent(""), "Chrome/123.0.0.0")
}

func TestDefaultUserAgentFallsBackWhenDetectionFails(t *testing.T) {
	originalGOOS := detectChromeVersionGOOS
	originalOutput := chromeVersionCommandOutput
	t.Cleanup(func() {
		detectChromeVersionGOOS = originalGOOS
		chromeVersionCommandOutput = originalOutput
	})

	detectChromeVersionGOOS = "linux"
	chromeVersionCommandOutput = func(candidate string) ([]byte, error) {
		require.Equal(t, "custom-chrome", candidate)
		return nil, errors.New("not installed")
	}

	require.Equal(t, "", DetectChromeVersion("custom-chrome"))
	require.Contains(t, DefaultUserAgent("custom-chrome"), "Chrome/130.0.0.0")
}
