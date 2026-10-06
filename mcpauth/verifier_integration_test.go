package mcpauth_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tyemirov/utils/mcpauth"
)

type accountPrincipal struct{ Account string }

func constructedToken(t *testing.T, binding string, expiration time.Time, scopes []string) *mcpauth.VerifiedToken[accountPrincipal] {
	t.Helper()
	token, err := mcpauth.NewVerifiedToken(binding, expiration, scopes, accountPrincipal{Account: "account-1"})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestHTTPAuthorization(t *testing.T) {
	operational := errors.New("JWKS unavailable: secret-credential")
	var reports []error
	var calls atomic.Int64
	var reportsMutex sync.Mutex
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{
		Validate: func(ctx context.Context, credential string, request *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
			calls.Add(1)
			if ctx != request.Context() {
				t.Error("request context changed")
			}
			switch credential {
			case "valid":
				return constructedToken(t, "grant-1", time.Now().Add(time.Hour), []string{"read"}), nil
			case "expired":
				return constructedToken(t, "grant-1", time.Now().Add(-time.Hour), []string{"read"}), nil
			case "expired-scope":
				return constructedToken(t, "grant-1", time.Now().Add(-time.Hour), nil), nil
			case "scope":
				return constructedToken(t, "grant-1", time.Now().Add(time.Hour), nil), nil
			case "rejected":
				return nil, fmt.Errorf("credential detail: %w", mcpauth.ErrUnauthenticated)
			case "operational":
				return nil, operational
			case "zero":
				return new(mcpauth.VerifiedToken[accountPrincipal]), nil
			default:
				return nil, nil
			}
		},
		ReportFailure: func(_ context.Context, failure error) {
			reportsMutex.Lock()
			defer reportsMutex.Unlock()
			reports = append(reports, failure)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.RequireBearerToken(verifier.Verify, &auth.RequireBearerTokenOptions{Scopes: []string{"read"}, ResourceMetadataURL: "https://example.test/metadata"})(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, err := mcpauth.Principal[accountPrincipal](auth.TokenInfoFromContext(request.Context()))
		if err != nil {
			t.Error(err)
			writer.WriteHeader(500)
			return
		}
		fmt.Fprint(writer, principal.Account)
	}))
	server := httptest.NewServer(handler)
	defer server.Close()
	cases := []struct {
		name, header, body          string
		status, reported, validated int
	}{
		{"missing", "", "no bearer token", 401, 0, 0},
		{"malformed", "Basic valid", "no bearer token", 401, 0, 0},
		{"extra fields", "Bearer valid extra", "no bearer token", 401, 0, 0},
		{"valid", "Bearer valid", "account-1", 200, 0, 1},
		{"expired", "Bearer expired", "token expired", 401, 0, 1},
		{"scope", "Bearer scope", "insufficient scope", 403, 0, 1},
		{"expired missing scope", "Bearer expired-scope", "insufficient scope", 403, 0, 1},
		{"rejected", "Bearer rejected", "invalid token", 401, 0, 1},
		{"operational", "Bearer operational", "token verification failed", 500, 1, 1},
		{"nil token", "Bearer nil", "token verification failed", 500, 1, 1},
		{"zero token", "Bearer zero", "token verification failed", 500, 1, 1},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			reportsMutex.Lock()
			beforeReports := len(reports)
			reportsMutex.Unlock()
			beforeCalls := calls.Load()
			request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			request.Header.Set("Authorization", scenario.header)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != scenario.status || strings.TrimSpace(string(body)) != scenario.body {
				t.Fatalf("status/body = %d %q", response.StatusCode, body)
			}
			reportsMutex.Lock()
			defer reportsMutex.Unlock()
			if len(reports)-beforeReports != scenario.reported || calls.Load()-beforeCalls != int64(scenario.validated) {
				t.Fatalf("reports/calls = %d/%d", len(reports)-beforeReports, calls.Load()-beforeCalls)
			}
			if scenario.status == 401 || scenario.status == 403 {
				if !strings.Contains(response.Header.Get("WWW-Authenticate"), `resource_metadata="https://example.test/metadata"`) {
					t.Fatal("metadata challenge missing")
				}
			}
			if scenario.name == "operational" && !errors.Is(reports[beforeReports], operational) {
				t.Fatal("diagnostic cause lost")
			}
		})
	}
}

type bearerTransport struct {
	credential atomic.Value
	base       http.RoundTripper
}

func (transport *bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+transport.credential.Load().(string))
	return transport.base.RoundTrip(clone)
}

func TestSDKPrincipalSessionBindingAndRefresh(t *testing.T) {
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{Validate: func(_ context.Context, credential string, _ *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		binding := "issuer/tenant/subject/client/grant"
		if credential == "other-client" {
			binding = "issuer/tenant/subject/other-client/grant"
		}
		if credential == "other-grant" {
			binding = "issuer/tenant/subject/client/other-grant"
		}
		return constructedToken(t, binding, time.Now().Add(time.Hour), []string{"read"}), nil
	}, ReportFailure: func(_ context.Context, err error) { t.Error(err) }})
	if err != nil {
		t.Fatal(err)
	}
	sdkServer := mcp.NewServer(&mcp.Implementation{Name: "principal", Version: "1"}, nil)
	mcp.AddTool(sdkServer, &mcp.Tool{Name: "identity", Description: "Read account"}, func(_ context.Context, request *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		principal, err := mcpauth.Principal[accountPrincipal](request.Extra.TokenInfo)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: principal.Account}}}, nil, nil
	})
	stream := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return sdkServer }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	var httpAccounts sync.Map
	observer := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, err := mcpauth.Principal[accountPrincipal](auth.TokenInfoFromContext(request.Context()))
		if err != nil {
			t.Error(err)
		}
		httpAccounts.Store(principal.Account, true)
		stream.ServeHTTP(writer, request)
	})
	server := httptest.NewServer(auth.RequireBearerToken(verifier.Verify, &auth.RequireBearerTokenOptions{Scopes: []string{"read"}})(observer))
	defer server.Close()
	transport := &bearerTransport{base: server.Client().Transport}
	transport.credential.Store("initial")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: transport}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, credential := range []string{"initial", "refreshed"} {
		transport.credential.Store(credential)
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "identity"})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError || result.Content[0].(*mcp.TextContent).Text != "account-1" {
			t.Fatalf("tool principal = %+v", result)
		}
	}
	if _, ok := httpAccounts.Load("account-1"); !ok {
		t.Fatal("HTTP principal missing")
	}
	for _, credential := range []string{"other-client", "other-grant"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"jsonrpc":"2.0","id":100,"method":"tools/list"}`))
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("Mcp-Session-Id", session.ID())
		request.Header.Set("MCP-Protocol-Version", "2025-11-25")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s session status = %d", credential, response.StatusCode)
		}
	}
}
