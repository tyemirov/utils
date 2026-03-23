package crawler

import "testing"

func TestResultCalculateScore(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                   string
		result                 Result
		inputConfiguredVerifer int
		expectedScore          int
	}{
		{
			name: "returns zero when result failed",
			result: Result{
				Success:                 false,
				ConfiguredVerifierCount: 4,
			},
			inputConfiguredVerifer: 0,
			expectedScore:          0,
		},
		{
			name: "uses stored verifier count when not provided",
			result: Result{
				Success:                 true,
				ConfiguredVerifierCount: 4,
				RuleResults: []RuleResult{
					{
						VerificationResults: []VerificationResult{
							{Passed: true},
							{Passed: true},
							{Passed: false},
							{Passed: false},
						},
					},
				},
			},
			inputConfiguredVerifer: 0,
			expectedScore:          50,
		},
		{
			name: "returns mismatch warning score zero when counts differ",
			result: Result{
				Success: true,
				RuleResults: []RuleResult{
					{
						VerificationResults: []VerificationResult{
							{Passed: true},
						},
					},
				},
			},
			inputConfiguredVerifer: 2,
			expectedScore:          0,
		},
		{
			name: "calculates percentage when counts match",
			result: Result{
				Success: true,
				RuleResults: []RuleResult{
					{
						VerificationResults: []VerificationResult{
							{Passed: true},
							{Passed: false},
						},
					},
				},
			},
			inputConfiguredVerifer: 2,
			expectedScore:          50,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			score := testCase.result.CalculateScore(testCase.inputConfiguredVerifer)
			if score != testCase.expectedScore {
				t.Fatalf("score mismatch: expected %d, got %d", testCase.expectedScore, score)
			}
		})
	}
}

func TestResolveImageStatus(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		status          ImageStatus
		productImageURL string
		expected        ImageStatus
	}{
		{
			name:            "keeps pending status",
			status:          ImageStatusPending,
			productImageURL: "/download/html/AMZN/A1/1700000000/A1.webp",
			expected:        ImageStatusPending,
		},
		{
			name:            "defaults to ready for populated image URL",
			status:          "",
			productImageURL: "/download/html/AMZN/A1/1700000000/A1.webp",
			expected:        ImageStatusReady,
		},
		{
			name:            "defaults to failed when image URL missing",
			status:          "",
			productImageURL: "",
			expected:        ImageStatusFailed,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual := ResolveImageStatus(testCase.status, testCase.productImageURL)
			if actual != testCase.expected {
				t.Fatalf("status mismatch: expected %s, got %s", testCase.expected, actual)
			}
		})
	}
}
