package gauss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tyemirov/utils/gauss"
	"github.com/tyemirov/utils/pointers"
	"golang.org/x/oauth2"
)

func TestNew(t *testing.T) {
	t.Run("empty client ID returns error", func(t *testing.T) {
		_, err := gauss.New(gauss.Config{
			ClientID:     "",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "client ID") {
			t.Fatalf("expected client ID error, got: %v", err)
		}

		_, err = gauss.New(gauss.Config{
			ClientID:     "   ",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "client ID") {
			t.Fatalf("expected client ID error, got: %v", err)
		}
	})

	t.Run("empty client secret returns error", func(t *testing.T) {
		_, err := gauss.New(gauss.Config{
			ClientID:     "client-id",
			ClientSecret: "",
			RedirectURL:  "https://example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "client secret") {
			t.Fatalf("expected client secret error, got: %v", err)
		}

		_, err = gauss.New(gauss.Config{
			ClientID:     "client-id",
			ClientSecret: "   ",
			RedirectURL:  "https://example.com/callback",
		})
		if err == nil || !strings.Contains(err.Error(), "client secret") {
			t.Fatalf("expected client secret error, got: %v", err)
		}
	})

	t.Run("empty redirect URL returns error", func(t *testing.T) {
		_, err := gauss.New(gauss.Config{
			ClientID:     "client-id",
			ClientSecret: "secret",
			RedirectURL:  "",
		})
		if err == nil || !strings.Contains(err.Error(), "redirect URL") {
			t.Fatalf("expected redirect URL error, got: %v", err)
		}

		_, err = gauss.New(gauss.Config{
			ClientID:     "client-id",
			ClientSecret: "secret",
			RedirectURL:  "   ",
		})
		if err == nil || !strings.Contains(err.Error(), "redirect URL") {
			t.Fatalf("expected redirect URL error, got: %v", err)
		}
	})

	t.Run("valid config with defaults", func(t *testing.T) {
		client, err := gauss.New(gauss.Config{
			ClientID:     "client-id",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}

		oauthCfg := client.OAuthConfig()
		if oauthCfg.ClientID != "client-id" {
			t.Errorf("expected client-id, got %s", oauthCfg.ClientID)
		}
		if oauthCfg.ClientSecret != "secret" {
			t.Errorf("expected secret, got %s", oauthCfg.ClientSecret)
		}
		if oauthCfg.RedirectURL != "https://example.com/callback" {
			t.Errorf("expected redirect URL, got %s", oauthCfg.RedirectURL)
		}
		if len(oauthCfg.Scopes) != 2 {
			t.Errorf("expected 2 default scopes, got %d", len(oauthCfg.Scopes))
		}
	})

	t.Run("valid config with custom scopes, endpoint, and userinfo URL", func(t *testing.T) {
		customEndpoint := oauth2.Endpoint{
			AuthURL:  "https://custom.test/auth",
			TokenURL: "https://custom.test/token",
		}
		client, err := gauss.New(gauss.Config{
			ClientID:     "custom-client",
			ClientSecret: "custom-secret",
			RedirectURL:  "http://localhost:8080/callback",
			Scopes:       []gauss.Scope{gauss.ScopeGmailModify},
			Endpoint:     &customEndpoint,
			UserInfoURL:  "https://custom.test/userinfo",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		oauthCfg := client.OAuthConfig()
		if oauthCfg.Endpoint.AuthURL != "https://custom.test/auth" {
			t.Errorf("expected custom auth URL, got %s", oauthCfg.Endpoint.AuthURL)
		}
		if len(oauthCfg.Scopes) != 1 || oauthCfg.Scopes[0] != string(gauss.ScopeGmailModify) {
			t.Errorf("expected ScopeGmailModify, got %v", oauthCfg.Scopes)
		}
	})
}

func TestAuthURL(t *testing.T) {
	t.Run("omitted offline requests offline access and consent", func(t *testing.T) {
		client, err := gauss.New(gauss.Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/oauth/callback",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		parsed, err := url.Parse(client.AuthURL("state-default"))
		if err != nil {
			t.Fatalf("failed to parse auth URL: %v", err)
		}
		query := parsed.Query()
		if query.Get("access_type") != "offline" {
			t.Errorf("expected access_type offline, got %q", query.Get("access_type"))
		}
		if query.Get("prompt") != "consent" {
			t.Errorf("expected prompt consent, got %q", query.Get("prompt"))
		}
	})

	t.Run("offline enabled adds offline access type and consent prompt", func(t *testing.T) {
		client, err := gauss.New(gauss.Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/oauth/callback",
			Offline:      pointers.FromBool(true),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rawURL := client.AuthURL("state-1234")
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("failed to parse auth URL: %v", err)
		}

		q := parsed.Query()
		if q.Get("state") != "state-1234" {
			t.Errorf("expected state state-1234, got %s", q.Get("state"))
		}
		if q.Get("access_type") != "offline" {
			t.Errorf("expected access_type offline, got %s", q.Get("access_type"))
		}
		if q.Get("prompt") != "consent" {
			t.Errorf("expected prompt consent, got %s", q.Get("prompt"))
		}
	})

	t.Run("offline disabled omits offline access type and consent prompt", func(t *testing.T) {
		client, err := gauss.New(gauss.Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://app.example.com/oauth/callback",
			Offline:      pointers.FromBool(false),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rawURL := client.AuthURL("state-online", oauth2.SetAuthURLParam("login_hint", "user@example.com"))
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("failed to parse auth URL: %v", err)
		}

		q := parsed.Query()
		if q.Get("state") != "state-online" {
			t.Errorf("expected state state-online, got %s", q.Get("state"))
		}
		if q.Has("access_type") {
			t.Errorf("expected no access_type, got %s", q.Get("access_type"))
		}
		if q.Has("prompt") {
			t.Errorf("expected no prompt, got %s", q.Get("prompt"))
		}
		if q.Get("login_hint") != "user@example.com" {
			t.Errorf("expected login_hint user@example.com, got %s", q.Get("login_hint"))
		}
	})
}

func TestExchange(t *testing.T) {
	t.Run("empty code returns error", func(t *testing.T) {
		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
		})

		_, err := client.Exchange(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "authorization code") {
			t.Fatalf("expected code error, got: %v", err)
		}

		_, err = client.Exchange(context.Background(), "   ")
		if err == nil || !strings.Contains(err.Error(), "authorization code") {
			t.Fatalf("expected code error, got: %v", err)
		}
	})

	t.Run("server error wrapped", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		}))
		defer tokenServer.Close()

		endpoint := oauth2.Endpoint{
			AuthURL:  tokenServer.URL + "/auth",
			TokenURL: tokenServer.URL + "/token",
		}

		client, err := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			Endpoint:     &endpoint,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.Exchange(context.Background(), "test-code")
		if err == nil || !strings.Contains(err.Error(), "exchange token") {
			t.Fatalf("expected exchange token error, got: %v", err)
		}
	})

	t.Run("successful exchange returns token", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-123",
				"token_type":    "Bearer",
				"refresh_token": "refresh-456",
				"expires_in":    3600,
			})
		}))
		defer tokenServer.Close()

		endpoint := oauth2.Endpoint{
			AuthURL:  tokenServer.URL + "/auth",
			TokenURL: tokenServer.URL + "/token",
		}

		client, err := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			Endpoint:     &endpoint,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		token, err := client.Exchange(context.Background(), "valid-code")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token.AccessToken != "access-123" {
			t.Errorf("expected access-123, got %s", token.AccessToken)
		}
		if token.RefreshToken != "refresh-456" {
			t.Errorf("expected refresh-456, got %s", token.RefreshToken)
		}
	})
}

func TestTokenSourceAndHTTPClient(t *testing.T) {
	client, _ := gauss.New(gauss.Config{
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "https://example.com/callback",
	})

	t.Run("nil token returns error for TokenSource", func(t *testing.T) {
		_, err := client.TokenSource(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "token must not be nil") {
			t.Fatalf("expected nil token error, got: %v", err)
		}
	})

	t.Run("valid token returns TokenSource", func(t *testing.T) {
		tok := &oauth2.Token{
			AccessToken: "abc",
			Expiry:      time.Now().Add(time.Hour),
		}
		ts, err := client.TokenSource(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		gotTok, err := ts.Token()
		if err != nil {
			t.Fatalf("unexpected token error: %v", err)
		}
		if gotTok.AccessToken != "abc" {
			t.Errorf("expected abc, got %s", gotTok.AccessToken)
		}
	})

	t.Run("nil token returns error for HTTPClient", func(t *testing.T) {
		_, err := client.HTTPClient(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "token must not be nil") {
			t.Fatalf("expected nil token error, got: %v", err)
		}
	})

	t.Run("valid token returns HTTPClient", func(t *testing.T) {
		tok := &oauth2.Token{
			AccessToken: "abc",
			Expiry:      time.Now().Add(time.Hour),
		}
		httpClient, err := client.HTTPClient(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if httpClient == nil {
			t.Fatal("expected non-nil HTTP client")
		}
	})
}

func TestFetchUserInfo(t *testing.T) {
	t.Run("nil token returns error", func(t *testing.T) {
		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
		})
		_, err := client.FetchUserInfo(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "token must not be nil") {
			t.Fatalf("expected nil token error, got: %v", err)
		}
	})

	t.Run("invalid request URL returns error", func(t *testing.T) {
		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			UserInfoURL:  "://invalid-url",
		})
		tok := &oauth2.Token{AccessToken: "token"}
		_, err := client.FetchUserInfo(context.Background(), tok)
		if err == nil || !strings.Contains(err.Error(), "new userinfo request") {
			t.Fatalf("expected request error, got: %v", err)
		}
	})

	t.Run("transport error returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		serverURL := server.URL
		server.Close() // immediately close to force network error

		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			UserInfoURL:  serverURL,
		})
		tok := &oauth2.Token{AccessToken: "token"}
		_, err := client.FetchUserInfo(context.Background(), tok)
		if err == nil || !strings.Contains(err.Error(), "fetch userinfo") {
			t.Fatalf("expected transport error, got: %v", err)
		}
	})

	t.Run("non-200 status returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			UserInfoURL:  server.URL,
		})
		tok := &oauth2.Token{AccessToken: "token"}
		_, err := client.FetchUserInfo(context.Background(), tok)
		if err == nil || !strings.Contains(err.Error(), "unexpected userinfo status 401") {
			t.Fatalf("expected 401 status error, got: %v", err)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			UserInfoURL:  server.URL,
		})
		tok := &oauth2.Token{AccessToken: "token"}
		_, err := client.FetchUserInfo(context.Background(), tok)
		if err == nil || !strings.Contains(err.Error(), "decode userinfo") {
			t.Fatalf("expected decode error, got: %v", err)
		}
	})

	t.Run("successful userinfo retrieval", func(t *testing.T) {
		expectedUser := gauss.GoogleUser{
			ID:            "google-user-id-999",
			Email:         "test@example.com",
			VerifiedEmail: true,
			Name:          "Test User",
			Picture:       "https://lh3.googleusercontent.com/photo.jpg",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer valid-token" {
				t.Errorf("expected Bearer valid-token, got %s", authHeader)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(expectedUser)
		}))
		defer server.Close()

		client, _ := gauss.New(gauss.Config{
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "https://example.com/callback",
			UserInfoURL:  server.URL,
		})
		tok := &oauth2.Token{AccessToken: "valid-token"}
		user, err := client.FetchUserInfo(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Email != expectedUser.Email {
			t.Errorf("expected email %s, got %s", expectedUser.Email, user.Email)
		}
		if user.Name != expectedUser.Name {
			t.Errorf("expected name %s, got %s", expectedUser.Name, user.Name)
		}
		if user.ID != expectedUser.ID {
			t.Errorf("expected ID %s, got %s", expectedUser.ID, user.ID)
		}
		if !user.VerifiedEmail {
			t.Errorf("expected verified email true")
		}
		if user.Picture != expectedUser.Picture {
			t.Errorf("expected picture %s, got %s", expectedUser.Picture, user.Picture)
		}
	})
}
