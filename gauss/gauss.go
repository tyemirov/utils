package gauss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// DefaultUserInfoEndpoint is the Google OAuth2 userinfo endpoint.
const DefaultUserInfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"

// GoogleUser contains user profile details returned by Google.
type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Config specifies credentials and settings for Google OAuth2.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []Scope
	// Offline controls whether to request a refresh token via offline access (default: true).
	Offline bool
	// Endpoint overrides the OAuth2 endpoints (defaults to google.Endpoint).
	Endpoint *oauth2.Endpoint
	// UserInfoURL overrides the userinfo endpoint (defaults to DefaultUserInfoEndpoint).
	UserInfoURL string
}

// Client encapsulates Google OAuth2 operations.
type Client struct {
	oauthConfig *oauth2.Config
	offline     bool
	userInfoURL string
}

// New creates a new Client after validating required configuration fields.
func New(cfg Config) (*Client, error) {
	trimmedClientID := strings.TrimSpace(cfg.ClientID)
	if trimmedClientID == "" {
		return nil, errors.New("gauss: client ID must not be empty")
	}

	trimmedClientSecret := strings.TrimSpace(cfg.ClientSecret)
	if trimmedClientSecret == "" {
		return nil, errors.New("gauss: client secret must not be empty")
	}

	trimmedRedirectURL := strings.TrimSpace(cfg.RedirectURL)
	if trimmedRedirectURL == "" {
		return nil, errors.New("gauss: redirect URL must not be empty")
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}

	endpoint := google.Endpoint
	if cfg.Endpoint != nil {
		endpoint = *cfg.Endpoint
	}

	userInfoURL := strings.TrimSpace(cfg.UserInfoURL)
	if userInfoURL == "" {
		userInfoURL = DefaultUserInfoEndpoint
	}

	oauthConfig := &oauth2.Config{
		ClientID:     trimmedClientID,
		ClientSecret: trimmedClientSecret,
		RedirectURL:  trimmedRedirectURL,
		Scopes:       ScopeStrings(scopes),
		Endpoint:     endpoint,
	}

	return &Client{
		oauthConfig: oauthConfig,
		offline:     cfg.Offline,
		userInfoURL: userInfoURL,
	}, nil
}

// OAuthConfig returns the underlying oauth2.Config.
func (c *Client) OAuthConfig() *oauth2.Config {
	return c.oauthConfig
}

// AuthURL generates the Google consent page URL.
// When Offline is enabled on the client, offline access and prompt=consent are automatically applied.
func (c *Client) AuthURL(state string, extraOpts ...oauth2.AuthCodeOption) string {
	opts := make([]oauth2.AuthCodeOption, 0, len(extraOpts)+2)
	if c.offline {
		opts = append(opts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	opts = append(opts, extraOpts...)
	return c.oauthConfig.AuthCodeURL(state, opts...)
}

// Exchange exchanges an authorization code for an OAuth2 token (including refresh token if offline).
func (c *Client) Exchange(ctx context.Context, code string, extraOpts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return nil, errors.New("gauss: authorization code must not be empty")
	}

	token, err := c.oauthConfig.Exchange(ctx, trimmedCode, extraOpts...)
	if err != nil {
		return nil, fmt.Errorf("gauss: exchange token: %w", err)
	}
	return token, nil
}

// TokenSource returns an auto-refreshing TokenSource initialized with the provided token.
func (c *Client) TokenSource(ctx context.Context, token *oauth2.Token) (oauth2.TokenSource, error) {
	if token == nil {
		return nil, errors.New("gauss: token must not be nil")
	}
	return c.oauthConfig.TokenSource(ctx, token), nil
}

// HTTPClient returns an *http.Client that automatically refreshes and injects OAuth2 bearer credentials.
func (c *Client) HTTPClient(ctx context.Context, token *oauth2.Token) (*http.Client, error) {
	tokenSource, err := c.TokenSource(ctx, token)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, tokenSource), nil
}

// FetchUserInfo retrieves user profile details using the authenticated client.
func (c *Client) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*GoogleUser, error) {
	httpClient, err := c.HTTPClient(ctx, token)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("gauss: new userinfo request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gauss: fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gauss: unexpected userinfo status %d", resp.StatusCode)
	}

	var user GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("gauss: decode userinfo: %w", err)
	}

	return &user, nil
}
