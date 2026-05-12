package crawler

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestProxyLeaseSelectorAcquiresDistinctLeasesAndReusesReleasedSuccess(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://provider-one-user-one.example:8080"},
				{Name: "user-two", URL: "http://provider-one-user-two.example:8080"},
			},
		},
		{
			Name: "provider-two",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://provider-two-user-one.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstLease := selector.Acquire()
	secondLease := selector.Acquire()
	thirdLease := selector.Acquire()

	require.Equal(t, "http://provider-one-user-one.example:8080", firstLease.ProxyURL)
	require.Equal(t, "http://provider-one-user-two.example:8080", secondLease.ProxyURL)
	require.Equal(t, "http://provider-two-user-one.example:8080", thirdLease.ProxyURL)

	selector.ReportSuccess(firstLease)
	reusedLease := selector.Acquire()

	require.Equal(t, firstLease.ProxyURL, reusedLease.ProxyURL)
}

func TestProxyLeaseSelectorReusesLeastReservedHealthyLeaseWhenAllReserved(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
		Name: "provider-one",
		Users: []ProxyRotationUserConfig{
			{Name: "user-one", URL: "http://provider-one-user-one.example:8080"},
			{Name: "user-two", URL: "http://provider-one-user-two.example:8080"},
		},
	}})
	require.NoError(t, err)

	firstLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	secondLease, err := selector.AcquireRequired()
	require.NoError(t, err)

	request, err := http.NewRequest(http.MethodGet, "https://example.com/product", nil)
	require.NoError(t, err)
	selectedProxyURL, err := selector.Select(request)
	require.NoError(t, err)
	require.Equal(t, firstLease.ProxyURL, selectedProxyURL.String())

	selectedLease, found := SelectedProxySelection(request)
	require.True(t, found)
	require.Equal(t, firstLease.ProxyURL, selectedLease.ProxyURL)

	leastReservedLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, secondLease.ProxyURL, leastReservedLease.ProxyURL)
}

func TestProxyLeaseSelectorAcquireForRequestReusesAttachedLease(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
		Name: "provider-one",
		Users: []ProxyRotationUserConfig{
			{Name: "user-one", URL: "http://provider-one-user-one.example:8080"},
			{Name: "user-two", URL: "http://provider-one-user-two.example:8080"},
		},
	}})
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodGet, "https://example.com/product", nil)
	require.NoError(t, err)
	firstLease, err := selector.AcquireForRequest(request)
	require.NoError(t, err)

	redirectRequest := request.Clone(request.Context())
	redirectLease, err := selector.AcquireForRequest(redirectRequest)
	require.NoError(t, err)

	require.Equal(t, firstLease, redirectLease)
	selector.ReportSuccess(firstLease)
	nextLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, firstLease.ProxyURL, nextLease.ProxyURL)
}

func TestProxyLeaseSelectorRotatesProviderImmediatelyAndAdvancesUserOnReturn(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "webshare",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://webshare-one.example:8080"},
				{Name: "user-two", URL: "http://webshare-two.example:8080"},
			},
		},
		{
			Name: "brightdata",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://brightdata-one.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstLease := selector.Acquire()
	require.Equal(t, "http://webshare-one.example:8080", firstLease.ProxyURL)

	selector.ReportFailure(firstLease)
	secondLease := selector.Acquire()
	require.Equal(t, "http://brightdata-one.example:8080", secondLease.ProxyURL)

	selector.ReportFailure(secondLease)
	thirdLease := selector.Acquire()
	require.Equal(t, "http://webshare-two.example:8080", thirdLease.ProxyURL)
}

func TestProxyLeaseSelectorIgnoresStaleReportsAfterGenerationChange(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://provider-one.example:8080"},
			},
		},
		{
			Name: "provider-two",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://provider-two.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstLease := selector.Acquire()
	selector.ReportFailure(firstLease)
	selector.ReportSuccess(firstLease)

	nextLease := selector.Acquire()
	require.Equal(t, "http://provider-two.example:8080", nextLease.ProxyURL)
}

func TestProxyLeaseSelectorAcquireRequiredAndFlatProxyFallback(t *testing.T) {
	t.Parallel()

	emptySelector, err := NewProxyLeaseSelector(nil)
	require.NoError(t, err)
	require.Nil(t, emptySelector)

	var nilSelector *ProxyLeaseSelector
	_, err = nilSelector.AcquireRequired()
	require.ErrorIs(t, err, ErrProxyLeaseUnavailable)

	flatSelector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "flat",
			Users: []ProxyRotationUserConfig{
				{Name: "proxy-one", URL: "http://proxy-one.example:8080"},
				{Name: "proxy-two", URL: "http://proxy-two.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstLease, err := flatSelector.AcquireRequired()
	require.NoError(t, err)
	require.True(t, firstLease.Valid())
	require.Equal(t, "http://proxy-one.example:8080", firstLease.ProxyURL)

	flatSelector.ReportFailure(firstLease)
	secondLease, err := flatSelector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "http://proxy-two.example:8080", secondLease.ProxyURL)
}

func TestProxyLeaseSelectorValidationDuplicateAndReleaseBranches(t *testing.T) {
	t.Parallel()

	_, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{Name: " "}})
	require.Error(t, err)

	_, err = NewProxyLeaseSelector([]ProxyRotationProviderConfig{{Name: "provider", Users: []ProxyRotationUserConfig{{Name: " ", URL: "http://proxy.example:8080"}}}})
	require.Error(t, err)

	_, err = NewProxyLeaseSelector([]ProxyRotationProviderConfig{{Name: "provider", Users: []ProxyRotationUserConfig{{Name: "user", URL: "://bad\x00proxy"}}}})
	require.Error(t, err)

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://shared.example:8080"},
				{Name: "user-two", URL: "http://unique.example:8080"},
			},
		},
		{
			Name: "provider-two",
			Users: []ProxyRotationUserConfig{
				{Name: "duplicate", URL: "http://shared.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	firstLease := selector.Acquire()
	secondLease := selector.Acquire()

	require.Equal(t, "http://shared.example:8080", firstLease.ProxyURL)
	require.Equal(t, "http://unique.example:8080", secondLease.ProxyURL)

	saturatedLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, firstLease.ProxyURL, saturatedLease.ProxyURL)

	selector.Release(firstLease)
	reusedLease := selector.Acquire()
	require.Equal(t, firstLease.ProxyURL, reusedLease.ProxyURL)
}

func TestProxyLeaseSelectorCooldownSkipsPausedLeasesAndExhausts(t *testing.T) {
	t.Parallel()

	currentTime := time.Unix(0, 0)
	selector, err := NewProxyLeaseSelectorWithOptions(
		[]ProxyRotationProviderConfig{
			{
				Name: "provider-one",
				Users: []ProxyRotationUserConfig{
					{Name: "user-one", URL: "http://provider-one.example:8080"},
					{Name: "user-two", URL: "http://provider-two.example:8080"},
				},
			},
		},
		ProxyLeaseSelectorCircuitBreaker(true),
		ProxyLeaseSelectorClock(func() time.Time { return currentTime }),
	)
	require.NoError(t, err)

	firstLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "http://provider-one.example:8080", firstLease.ProxyURL)

	selector.ReportCriticalFailure(firstLease)
	require.False(t, selector.IsAvailable(firstLease.ProxyURL))
	require.True(t, selector.IsAvailable(""))
	selector.RecordCriticalFailure("http://unknown.example:8080")

	secondLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "http://provider-two.example:8080", secondLease.ProxyURL)

	selector.ReportCriticalFailure(secondLease)
	_, err = selector.AcquireRequired()
	require.ErrorIs(t, err, ErrProxyLeaseCandidatesExhausted)

	currentTime = currentTime.Add(defaultProxyCooldownBase + time.Second)
	recoveredLease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "http://provider-one.example:8080", recoveredLease.ProxyURL)
	selector.ReportSuccess(recoveredLease)
	require.True(t, selector.IsAvailable(recoveredLease.ProxyURL))

	recoveredLease, err = selector.AcquireRequired()
	require.NoError(t, err)
	selector.RecordCriticalFailure(recoveredLease.ProxyURL)
	require.False(t, selector.IsAvailable(recoveredLease.ProxyURL))
}

func TestProxyLeaseSelectorStartsAtConfiguredProvider(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelectorWithOptions(
		[]ProxyRotationProviderConfig{
			{
				Name: "provider-one",
				Users: []ProxyRotationUserConfig{
					{Name: "user-one", URL: "http://provider-one.example:8080"},
				},
			},
			{
				Name: "provider-two",
				Users: []ProxyRotationUserConfig{
					{Name: "user-one", URL: "http://provider-two.example:8080"},
				},
			},
		},
		ProxyLeaseSelectorStartProvider(1),
	)
	require.NoError(t, err)

	lease, err := selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "provider-two", lease.ProviderName)
	require.Equal(t, "http://provider-two.example:8080", lease.ProxyURL)

	selector, err = NewProxyLeaseSelectorWithOptions(
		[]ProxyRotationProviderConfig{
			{
				Name: "provider-one",
				Users: []ProxyRotationUserConfig{
					{Name: "user-one", URL: "http://provider-one.example:8080"},
				},
			},
		},
		ProxyLeaseSelectorStartProvider(-1),
	)
	require.NoError(t, err)
	lease, err = selector.AcquireRequired()
	require.NoError(t, err)
	require.Equal(t, "provider-one", lease.ProviderName)
	require.Equal(t, 0, normalizeProxyProviderIndex(0, 0))
}

func TestProxyLeaseSelectorRecordsNonCriticalFailureWithCooldownTracker(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelectorWithOptions(
		[]ProxyRotationProviderConfig{{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{{
				Name: "user-one",
				URL:  "http://provider-one.example:8080",
			}},
		}},
		ProxyLeaseSelectorCircuitBreaker(true),
	)
	require.NoError(t, err)
	lease, err := selector.AcquireRequired()
	require.NoError(t, err)

	selector.ReportFailure(lease)
}

func TestProxyLeaseSelectorAcquireForRequestAttachesSelection(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://proxy.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	lease, err := selector.AcquireForRequest(request)
	require.NoError(t, err)
	require.Equal(t, "http://proxy.example:8080", lease.ProxyURL)

	selectedLease, found := SelectedProxySelection(request)
	require.True(t, found)
	require.Equal(t, lease, selectedLease)
	require.Equal(t, lease.ProxyURL, request.Context().Value(colly.ProxyURLKey))
}

func TestProxyLeaseSelectorCompatibilityStringReports(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "provider-one",
			Users: []ProxyRotationUserConfig{
				{Name: "user-one", URL: "http://provider-one.example:8080"},
			},
		},
	})
	require.NoError(t, err)

	lease := selector.Acquire()
	selector.ReportFailure(ProxyLease{})
	selector.ReportSuccess(ProxyLease{})
	selector.RecordFailure("http://unknown.example:8080")
	selector.RecordSuccess("http://unknown.example:8080")
	selector.RecordCriticalFailure("http://unknown.example:8080")
	var nilSelector *ProxyLeaseSelector
	nilSelector.RecordCriticalFailure("http://unknown.example:8080")
	selector.RecordSuccess(lease.ProxyURL)

	reusedLease := selector.Acquire()
	require.Equal(t, lease.ProxyURL, reusedLease.ProxyURL)
}

func TestProxyLeaseAttemptScopeSkipsFailedLeasesUntilExhausted(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{
		{
			Name: "brightdata",
			Users: []ProxyRotationUserConfig{
				{Name: "brightdata-one", URL: "http://brightdata-one.example:8080"},
			},
		},
		{
			Name: "iproyal",
			Users: []ProxyRotationUserConfig{
				{Name: "iproyal-one", URL: "http://iproyal-one.example:8080"},
			},
		},
		{
			Name: "webshare",
			Users: []ProxyRotationUserConfig{
				{Name: "webshare-one", URL: "http://webshare-one.example:8080"},
				{Name: "webshare-two", URL: "http://webshare-two.example:8080"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 4, selector.CandidateCount())

	attemptScope := NewProxyLeaseAttemptScope()
	var proxyURLs []string
	for candidateIndex := 0; candidateIndex < selector.CandidateCount(); candidateIndex++ {
		lease, err := attemptScope.AcquireRequired(selector)
		require.NoError(t, err)
		proxyURLs = append(proxyURLs, lease.ProxyURL)
		require.False(t, attemptScope.Failed(lease))

		attemptScope.ReportFailure(lease)
		selector.ReportFailure(lease)
		require.True(t, attemptScope.Failed(lease))
	}

	require.Equal(t, []string{
		"http://brightdata-one.example:8080",
		"http://iproyal-one.example:8080",
		"http://webshare-one.example:8080",
		"http://webshare-two.example:8080",
	}, proxyURLs)
	require.True(t, attemptScope.Exhausted(selector.CandidateCount()))

	_, err = attemptScope.AcquireRequired(selector)
	require.ErrorIs(t, err, ErrProxyLeaseCandidatesExhausted)
	require.ErrorContains(t, err, "all 4 configured proxy candidate(s) failed")
}

func TestProxyLeaseAttemptScopeHandlesNilAndInvalidBranches(t *testing.T) {
	t.Parallel()

	var nilScope *ProxyLeaseAttemptScope
	require.False(t, nilScope.Failed(ProxyLease{ProxyURL: "http://proxy.example:8080"}))
	require.False(t, nilScope.Exhausted(1))
	require.NotPanics(t, func() { nilScope.ReportFailure(ProxyLease{ProxyURL: "http://proxy.example:8080"}) })

	scope := NewProxyLeaseAttemptScope()
	scope.ReportFailure(ProxyLease{})
	require.False(t, scope.Failed(ProxyLease{}))
	require.False(t, scope.Exhausted(0))

	zeroValueScope := ProxyLeaseAttemptScope{}
	zeroValueLease := ProxyLease{ProxyURL: "http://zero-value.example:8080"}
	zeroValueScope.ReportFailure(zeroValueLease)
	require.True(t, zeroValueScope.Failed(zeroValueLease))

	var nilSelector *ProxyLeaseSelector
	_, err := scope.AcquireRequired(nilSelector)
	require.ErrorIs(t, err, ErrProxyLeaseUnavailable)
	require.Equal(t, 0, nilSelector.CandidateCount())
	require.ErrorIs(t, ProxyLeaseCandidatesExhaustedError(2), ErrProxyLeaseCandidatesExhausted)
}

func TestProxyLeaseUnavailableErrorWraps(t *testing.T) {
	t.Parallel()

	var selector *ProxyLeaseSelector
	_, err := selector.AcquireRequired()
	require.True(t, errors.Is(err, ErrProxyLeaseUnavailable))
}

func TestScraperConfigProxyCandidateCountPrefersSelector(t *testing.T) {
	t.Parallel()

	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
		Name: "provider-one",
		Users: []ProxyRotationUserConfig{
			{Name: "user-one", URL: "http://proxy-one.example:8080"},
			{Name: "user-two", URL: "http://proxy-two.example:8080"},
		},
	}})
	require.NoError(t, err)

	require.Equal(t, 2, (ScraperConfig{ProxyLeaseSelector: selector, ProxyList: []string{"http://legacy.example:8080"}}).ProxyCandidateCount())
	require.Equal(t, 1, (ScraperConfig{ProxyList: []string{"http://legacy.example:8080"}}).ProxyCandidateCount())
}

func TestProxyLeaseSelectorNilInvalidAndInternalFallbackBranches(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	var nilSelector *ProxyLeaseSelector
	_, err = nilSelector.AcquireForRequest(request)
	require.ErrorIs(t, err, ErrProxyLeaseUnavailable)
	require.NotPanics(t, func() { nilSelector.Release(ProxyLease{ProxyURL: "http://proxy.example:8080"}) })

	selection, found := nilSelector.SelectionForProxyURL("http://proxy.example:8080")
	require.False(t, found)
	require.Empty(t, selection)

	selector := &ProxyLeaseSelector{
		providers: []proxyRotationProvider{
			{name: "empty"},
			{
				name: "provider-one",
				users: []proxyRotationUser{
					{name: "user-one", raw: "http://proxy-one.example:8080"},
				},
				nextUser: 99,
			},
		},
		activeProvider: 99,
		reservations: map[string]int{
			"http://proxy-one.example:8080": 1,
		},
	}

	invalidLease, found := selector.firstUnreservedLeaseFromProviderLocked(-1)
	require.False(t, found)
	require.False(t, invalidLease.Valid())

	selector.reservations = map[string]int{}
	unreservedLease, found := selector.firstUnreservedLeaseFromProviderLocked(1)
	require.True(t, found)
	require.Equal(t, "http://proxy-one.example:8080", unreservedLease.ProxyURL)

	selector.reservations = map[string]int{"http://proxy-one.example:8080": 1}
	lease := selector.Acquire()
	require.True(t, lease.Valid())
	require.Equal(t, "http://proxy-one.example:8080", lease.ProxyURL)

	emptySelector := &ProxyLeaseSelector{reservations: map[string]int{}}
	require.False(t, emptySelector.Acquire().Valid())
	require.False(t, emptySelector.acquireLocked().Valid())

	selection, found = selector.SelectionForProxyURL(" ")
	require.False(t, found)
	require.Empty(t, selection)
	selection, found = selector.SelectionForProxyURL("http://unknown.example:8080")
	require.False(t, found)
	require.Empty(t, selection)

	rotationSelector := &ProxyRotationSelector{leaseSelector: &ProxyLeaseSelector{
		providers: []proxyRotationProvider{{
			name: "provider-one",
			users: []proxyRotationUser{{
				name: "user-one",
				raw:  "://bad\x00proxy",
			}},
		}},
		reservations: map[string]int{},
	}}
	proxyURL, err := rotationSelector.Select(request)
	require.Error(t, err)
	require.Nil(t, proxyURL)

	selector.recordFailureLocked("http://proxy-one.example:8080")
	selector.recordSuccessLocked("http://proxy-one.example:8080")
	selector.reservations = map[string]int{"http://proxy-one.example:8080": 2}
	selector.Release(ProxyLease{ProxyURL: "http://proxy-one.example:8080"})
	require.Equal(t, 1, selector.reservations["http://proxy-one.example:8080"])
}
