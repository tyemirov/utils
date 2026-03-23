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
