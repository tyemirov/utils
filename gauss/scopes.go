// Package gauss provides Google OAuth2 authorization helpers and bounded scopes.
package gauss

// Scope represents a Google OAuth2 scope string.
type Scope string

const (
	// ScopeEmail allows retrieving the user's email address.
	ScopeEmail Scope = "https://www.googleapis.com/auth/userinfo.email"
	// ScopeProfile allows retrieving basic profile information.
	ScopeProfile Scope = "https://www.googleapis.com/auth/userinfo.profile"
	// ScopeOpenID allows verifying the user's Google account identity.
	ScopeOpenID Scope = "openid"

	// ScopeGmailModify allows reading, composing, sending, and modifying Gmail messages and labels.
	ScopeGmailModify Scope = "https://www.googleapis.com/auth/gmail.modify"
	// ScopeGmailReadonly allows read-only access to Gmail resources.
	ScopeGmailReadonly Scope = "https://www.googleapis.com/auth/gmail.readonly"
	// ScopeGmailLabels allows creating, reading, updating, and deleting Gmail labels.
	ScopeGmailLabels Scope = "https://www.googleapis.com/auth/gmail.labels"
	// ScopeGmailSend allows sending messages only.
	ScopeGmailSend Scope = "https://www.googleapis.com/auth/gmail.send"

	// ScopeYouTube allows full management of YouTube resources.
	ScopeYouTube Scope = "https://www.googleapis.com/auth/youtube"
	// ScopeYouTubeReadonly allows read-only access to YouTube resources.
	ScopeYouTubeReadonly Scope = "https://www.googleapis.com/auth/youtube.readonly"
	// ScopeYouTubeUpload allows uploading videos to YouTube.
	ScopeYouTubeUpload Scope = "https://www.googleapis.com/auth/youtube.upload"
)

// DefaultScopes lists the standard identity scopes used when none are explicitly provided.
var DefaultScopes = []Scope{ScopeProfile, ScopeEmail}

// ScopeStrings converts a slice of Scope values into their string representations.
func ScopeStrings(scopes []Scope) []string {
	if len(scopes) == 0 {
		return nil
	}
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}
