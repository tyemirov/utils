package gauss_test

import (
	"testing"

	"github.com/tyemirov/utils/gauss"
)

func TestScopeStrings(t *testing.T) {
	t.Run("empty or nil slice returns nil", func(t *testing.T) {
		if out := gauss.ScopeStrings(nil); out != nil {
			t.Errorf("expected nil for nil input, got %v", out)
		}
		if out := gauss.ScopeStrings([]gauss.Scope{}); out != nil {
			t.Errorf("expected nil for empty input, got %v", out)
		}
	})

	t.Run("converts scopes to strings correctly", func(t *testing.T) {
		input := []gauss.Scope{
			gauss.ScopeEmail,
			gauss.ScopeProfile,
			gauss.ScopeOpenID,
			gauss.ScopeGmailModify,
			gauss.ScopeGmailReadonly,
			gauss.ScopeGmailLabels,
			gauss.ScopeGmailSend,
			gauss.ScopeYouTube,
			gauss.ScopeYouTubeReadonly,
			gauss.ScopeYouTubeUpload,
		}

		expected := []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"openid",
			"https://www.googleapis.com/auth/gmail.modify",
			"https://www.googleapis.com/auth/gmail.readonly",
			"https://www.googleapis.com/auth/gmail.labels",
			"https://www.googleapis.com/auth/gmail.send",
			"https://www.googleapis.com/auth/youtube",
			"https://www.googleapis.com/auth/youtube.readonly",
			"https://www.googleapis.com/auth/youtube.upload",
		}

		result := gauss.ScopeStrings(input)
		if len(result) != len(expected) {
			t.Fatalf("expected %d elements, got %d", len(expected), len(result))
		}

		for i, exp := range expected {
			if result[i] != exp {
				t.Errorf("index %d: expected %s, got %s", i, exp, result[i])
			}
		}
	})

	t.Run("default scopes contains profile and email", func(t *testing.T) {
		defaultStr := gauss.ScopeStrings(gauss.DefaultScopes)
		if len(defaultStr) != 2 {
			t.Fatalf("expected 2 default scopes, got %d", len(defaultStr))
		}
		if defaultStr[0] != string(gauss.ScopeProfile) || defaultStr[1] != string(gauss.ScopeEmail) {
			t.Errorf("unexpected default scopes: %v", defaultStr)
		}
	})
}
