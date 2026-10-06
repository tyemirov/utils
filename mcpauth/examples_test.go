package mcpauth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/tyemirov/utils/mcpauth"
)

func ExampleNewVerifier() {
	type accountPrincipal struct{ Account string }
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{
		Validate: func(_ context.Context, credential string, _ *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
			if credential != "accepted-credential" {
				return nil, mcpauth.ErrUnauthenticated
			}
			return mcpauth.NewVerifiedToken("issuer/tenant/subject/client/grant", time.Now().Add(time.Hour), []string{"read"}, accountPrincipal{Account: "account-1"})
		},
		ReportFailure: func(context.Context, error) { fmt.Println("token verification operation failed") },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	info, err := verifier.Verify(context.Background(), "accepted-credential", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	principal, err := mcpauth.Principal[accountPrincipal](info)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(principal.Account, info.UserID, info.Scopes)
	// Output: account-1 issuer/tenant/subject/client/grant [read]
}

func ExamplePrincipal() {
	type accountPrincipal struct{ Account string }
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{
		Validate: func(_ context.Context, credential string, _ *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
			if credential != "accepted-credential" {
				return nil, mcpauth.ErrUnauthenticated
			}
			return mcpauth.NewVerifiedToken("issuer/tenant/subject/client/grant", time.Now().Add(time.Hour), []string{"read"}, accountPrincipal{Account: "account-1"})
		},
		ReportFailure: func(context.Context, error) { fmt.Println("token verification operation failed") },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	handler := auth.RequireBearerToken(verifier.Verify, &auth.RequireBearerTokenOptions{Scopes: []string{"read"}})(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, err := mcpauth.Principal[accountPrincipal](auth.TokenInfoFromContext(request.Context()))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(writer, principal.Account)
	}))
	request := httptest.NewRequest(http.MethodGet, "https://example.test/mcp", nil)
	request.Header.Set("Authorization", "Bearer accepted-credential")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	fmt.Println(response.Code, response.Body.String())
	// Output: 200 account-1
}
