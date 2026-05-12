package crawler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestProxyRotationSelectorKeepsSuccessfulProviderUserSticky(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "webshare",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://webshare-1.example:8080"},
				{Name: "user-2", URL: "http://webshare-2.example:8080"},
			},
		},
		{
			Name: "brightdata",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://brightdata-1.example:8080"},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, selector)

	firstRequest := newProxyRotationRequest(t, "https://example.com/one")
	firstProxy, err := selector.Select(firstRequest)
	require.NoError(t, err)
	require.Equal(t, "http://webshare-1.example:8080", firstProxy.String())

	firstSelection, found := SelectedProxySelection(firstRequest)
	require.True(t, found)
	require.Equal(t, "webshare", firstSelection.ProviderName)
	require.Equal(t, "user-1", firstSelection.UserName)
	require.Equal(t, "http://webshare-1.example:8080", firstRequest.Context().Value(colly.ProxyURLKey))

	selector.RecordProxySuccess(firstSelection)

	secondRequest := newProxyRotationRequest(t, "https://example.com/two")
	secondProxy, err := selector.Select(secondRequest)
	require.NoError(t, err)
	require.Equal(t, "http://webshare-1.example:8080", secondProxy.String())
}

func TestProxyRotationSelectorRotatesProviderFirstAndAdvancesUsersOnReturn(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "webshare",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://webshare-1.example:8080"},
				{Name: "user-2", URL: "http://webshare-2.example:8080"},
			},
		},
		{
			Name: "brightdata",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://brightdata-1.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstRequest := newProxyRotationRequest(t, "https://example.com/one")
	firstProxy, err := selector.Select(firstRequest)
	require.NoError(t, err)
	require.Equal(t, "http://webshare-1.example:8080", firstProxy.String())
	firstSelection, found := SelectedProxySelection(firstRequest)
	require.True(t, found)

	selector.RecordProxyFailure(firstSelection)

	secondRequest := newProxyRotationRequest(t, "https://example.com/two")
	secondProxy, err := selector.Select(secondRequest)
	require.NoError(t, err)
	require.Equal(t, "http://brightdata-1.example:8080", secondProxy.String())
	secondSelection, found := SelectedProxySelection(secondRequest)
	require.True(t, found)

	selector.RecordProxyFailure(secondSelection)

	thirdRequest := newProxyRotationRequest(t, "https://example.com/three")
	thirdProxy, err := selector.Select(thirdRequest)
	require.NoError(t, err)
	require.Equal(t, "http://webshare-2.example:8080", thirdProxy.String())
}

func TestProxyRotationSelectorAdvancesUsersWhenOnlyOneProviderExists(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "iproyal",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://iproyal-1.example:8080"},
				{Name: "user-2", URL: "http://iproyal-2.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstRequest := newProxyRotationRequest(t, "https://example.com/one")
	firstProxy, err := selector.Select(firstRequest)
	require.NoError(t, err)
	require.Equal(t, "http://iproyal-1.example:8080", firstProxy.String())
	firstSelection, found := SelectedProxySelection(firstRequest)
	require.True(t, found)

	selector.RecordProxyFailure(firstSelection)

	secondRequest := newProxyRotationRequest(t, "https://example.com/two")
	secondProxy, err := selector.Select(secondRequest)
	require.NoError(t, err)
	require.Equal(t, "http://iproyal-2.example:8080", secondProxy.String())
	secondSelection, found := SelectedProxySelection(secondRequest)
	require.True(t, found)

	selector.RecordProxySuccess(secondSelection)

	thirdRequest := newProxyRotationRequest(t, "https://example.com/three")
	thirdProxy, err := selector.Select(thirdRequest)
	require.NoError(t, err)
	require.Equal(t, "http://iproyal-2.example:8080", thirdProxy.String())
}

func TestProxyRotationSelectorIgnoresStaleSuccessAfterFailureRotatesGeneration(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "webshare",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://webshare-1.example:8080"},
			},
		},
		{
			Name: "brightdata",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://brightdata-1.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstRequest := newProxyRotationRequest(t, "https://example.com/one")
	_, err = selector.Select(firstRequest)
	require.NoError(t, err)
	firstSelection, found := SelectedProxySelection(firstRequest)
	require.True(t, found)

	selector.RecordProxyFailure(firstSelection)
	selector.RecordProxySuccess(firstSelection)

	secondRequest := newProxyRotationRequest(t, "https://example.com/two")
	secondProxy, err := selector.Select(secondRequest)
	require.NoError(t, err)
	require.Equal(t, "http://brightdata-1.example:8080", secondProxy.String())
}

func TestProxyRotationSelectorSkipsDuplicateProxyURLs(t *testing.T) {
	t.Parallel()

	const sharedProxyURL = "http://shared.example:8080"
	const backupProxyURL = "http://backup.example:8080"

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-a",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: sharedProxyURL},
			},
		},
		{
			Name: "provider-b",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: sharedProxyURL},
			},
		},
		{
			Name: "backup",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: backupProxyURL},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, selector)

	firstRequest := newProxyRotationRequest(t, "https://example.com/one")
	firstProxy, err := selector.Select(firstRequest)
	require.NoError(t, err)
	require.Equal(t, sharedProxyURL, firstProxy.String())

	firstSelection, found := SelectedProxySelection(firstRequest)
	require.True(t, found)
	require.Equal(t, "provider-a", firstSelection.ProviderName)

	selector.RecordProxyFailure(firstSelection)

	secondRequest := newProxyRotationRequest(t, "https://example.com/two")
	secondProxy, err := selector.Select(secondRequest)
	require.NoError(t, err)
	require.Equal(t, backupProxyURL, secondProxy.String())
}

func TestProxyRotationSelectorStringHelpers(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-a",
			Users: []ProxyRotationUserConfig{
				{Name: "user-1", URL: "http://provider-a-1.example:8080"},
				{Name: "user-2", URL: "http://provider-a-2.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	selector.RecordFailure("http://provider-a-1.example:8080")
	request := newProxyRotationRequest(t, "https://example.com/two")
	proxyURL, err := selector.Select(request)
	require.NoError(t, err)
	require.Equal(t, "http://provider-a-2.example:8080", proxyURL.String())

	selector.RecordSuccess("http://provider-a-2.example:8080")
	request = newProxyRotationRequest(t, "https://example.com/three")
	proxyURL, err = selector.Select(request)
	require.NoError(t, err)
	require.Equal(t, "http://provider-a-2.example:8080", proxyURL.String())
}

func TestNewProxyRotationSelectorValidationAndEmptyInputs(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyRotationSelector(nil)
	require.NoError(t, err)
	require.Nil(t, selector)

	selector, err = NewProxyRotationSelector([]ProxyRotationProviderConfig{{Name: "empty", Users: []ProxyRotationUserConfig{{Name: "user-1"}}}})
	require.NoError(t, err)
	require.Nil(t, selector)

	_, err = NewProxyRotationSelector([]ProxyRotationProviderConfig{{Name: " "}})
	require.ErrorContains(t, err, "provider name must not be empty")

	_, err = NewProxyRotationSelector([]ProxyRotationProviderConfig{{Name: "provider", Users: []ProxyRotationUserConfig{{Name: " ", URL: "http://proxy.example:8080"}}}})
	require.ErrorContains(t, err, "user with empty name")

	_, err = NewProxyRotationSelector([]ProxyRotationProviderConfig{{Name: "provider", Users: []ProxyRotationUserConfig{{Name: "user", URL: "://bad\x00proxy"}}}})
	require.ErrorContains(t, err, "invalid proxy URL")
}

func TestProxyRotationSelectorNilEmptyAndUnknownBranches(t *testing.T) {
	t.Parallel()

	var nilSelector *ProxyRotationSelector
	proxyURL, err := nilSelector.Select(nil)
	require.NoError(t, err)
	require.Nil(t, proxyURL)
	require.NotPanics(t, func() { nilSelector.RecordFailure("http://unknown.example:8080") })
	require.NotPanics(t, func() { nilSelector.RecordSuccess("http://unknown.example:8080") })
	require.NotPanics(t, func() { nilSelector.RecordProxyFailure(ProxySelection{ProxyURL: "http://unknown.example:8080"}) })
	require.NotPanics(t, func() {
		nilSelector.RecordProxyCriticalFailure(ProxySelection{ProxyURL: "http://unknown.example:8080"})
	})
	require.NotPanics(t, func() { nilSelector.RecordProxySuccess(ProxySelection{ProxyURL: "http://unknown.example:8080"}) })
	selection, found := nilSelector.SelectionForProxyURL("http://unknown.example:8080")
	require.False(t, found)
	require.Empty(t, selection)

	emptySelector := &ProxyRotationSelector{}
	proxyURL, err = emptySelector.Select(newProxyRotationRequest(t, "https://example.com/empty"))
	require.NoError(t, err)
	require.Nil(t, proxyURL)

	emptyProviderSelector := &ProxyRotationSelector{leaseSelector: &ProxyLeaseSelector{
		providers:    []proxyRotationProvider{{name: "empty"}},
		reservations: map[string]int{},
	}}
	proxyURL, err = emptyProviderSelector.Select(newProxyRotationRequest(t, "https://example.com/empty-provider"))
	require.NoError(t, err)
	require.Nil(t, proxyURL)

	selector, err := NewProxyRotationSelector([]ProxyRotationProviderConfig{
		{Name: "provider-a", Users: []ProxyRotationUserConfig{{Name: "user-1", URL: "http://provider-a.example:8080"}}},
		{Name: "provider-b", Users: []ProxyRotationUserConfig{{Name: "user-1", URL: "http://provider-b.example:8080"}}},
	})
	require.NoError(t, err)
	require.NotNil(t, selector)

	selector.RecordFailure(" ")
	selector.RecordSuccess(" ")
	selector.RecordFailure("http://unknown.example:8080")
	selector.RecordSuccess("http://unknown.example:8080")
	selector.RecordProxyFailure(ProxySelection{})
	selector.RecordProxyCriticalFailure(ProxySelection{})
	selector.RecordProxySuccess(ProxySelection{})
	selector.RecordProxyFailure(ProxySelection{ProxyURL: "http://unknown.example:8080"})
	selector.RecordProxySuccess(ProxySelection{ProxyURL: "http://unknown.example:8080"})
	selector.RecordProxyFailure(ProxySelection{ProxyURL: "http://provider-a.example:8080", Generation: 99})
	selector.RecordProxySuccess(ProxySelection{ProxyURL: "http://provider-a.example:8080", Generation: 99})

	providerBSelection, found := selector.SelectionForProxyURL("http://provider-b.example:8080")
	require.True(t, found)
	selector.RecordProxyFailure(providerBSelection)
	selector.RecordProxySuccess(providerBSelection)

	selection, found = selector.SelectionForProxyURL(" ")
	require.False(t, found)
	require.Empty(t, selection)
	selection, found = selector.SelectionForProxyURL("http://unknown.example:8080")
	require.False(t, found)
	require.Empty(t, selection)

	request := newProxyRotationRequest(t, "https://example.com/no-selection")
	selection, found = SelectedProxySelection(request)
	require.False(t, found)
	require.Empty(t, selection)
	selection, found = SelectedProxySelection(nil)
	require.False(t, found)
	require.Empty(t, selection)
	AttachProxySelection(nil, ProxySelection{ProxyURL: "http://provider-a.example:8080"})
	AttachProxySelection(request, ProxySelection{})
	selection, found = SelectedProxySelection(request)
	require.False(t, found)
	require.Empty(t, selection)
	emptySelectionRequest := request.WithContext(context.WithValue(request.Context(), proxySelectionContextKey{}, ProxySelection{}))
	selection, found = SelectedProxySelection(emptySelectionRequest)
	require.False(t, found)
	require.Empty(t, selection)
}

func newProxyRotationRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	return request
}
