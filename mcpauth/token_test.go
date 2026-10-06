package mcpauth_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/tyemirov/utils/mcpauth"
)

func TestConstructionAndPrincipalBoundaries(t *testing.T) {
	validate := func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		return nil, nil
	}
	report := func(context.Context, error) {}
	for _, config := range []mcpauth.Config[accountPrincipal]{{}, {Validate: validate}, {ReportFailure: report}} {
		if verifier, err := mcpauth.NewVerifier(config); err == nil || verifier != nil {
			t.Fatal("invalid verifier constructed")
		}
	}
	for _, binding := range []string{"", " \t\n"} {
		if token, err := mcpauth.NewVerifiedToken(binding, time.Now(), nil, accountPrincipal{}); err == nil || token != nil {
			t.Fatal("empty binding accepted")
		}
	}
	if token, err := mcpauth.NewVerifiedToken("binding", time.Time{}, nil, accountPrincipal{}); err == nil || token != nil {
		t.Fatal("zero expiration accepted")
	}
	for _, info := range []*auth.TokenInfo{nil, {}, {Extra: map[string]any{"principal": accountPrincipal{}}}} {
		if _, err := mcpauth.Principal[accountPrincipal](info); err == nil {
			t.Fatal("absent principal accepted")
		}
	}
	token := constructedToken(t, " exact binding ", time.Now().Add(time.Hour), nil)
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{Validate: func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		return token, nil
	}, ReportFailure: report})
	if err != nil {
		t.Fatal(err)
	}
	info, err := verifier.Verify(context.Background(), "credential", nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.UserID != " exact binding " {
		t.Fatal("binding normalized")
	}
	if _, err := mcpauth.Principal[string](info); err == nil {
		t.Fatal("wrong principal type accepted")
	}
	var nilVerifier *mcpauth.Verifier[accountPrincipal]
	for _, invalid := range []*mcpauth.Verifier[accountPrincipal]{nilVerifier, new(mcpauth.Verifier[accountPrincipal])} {
		if info, err := invalid.Verify(context.Background(), "credential", nil); err == nil || info != nil || err.Error() != "token verification failed" {
			t.Fatal("unconstructed verifier accepted")
		}
	}
}

func TestScopeCopiesAndConcurrentConversion(t *testing.T) {
	expiration := time.Now().Add(time.Hour)
	scopes := []string{"read", "write"}
	token := constructedToken(t, "binding", expiration, scopes)
	scopes[0] = "mutated"
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{Validate: func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		return token, nil
	}, ReportFailure: func(context.Context, error) { t.Error("unexpected report") }})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 40 {
		group.Go(func() {
			info, err := verifier.Verify(context.Background(), "credential", nil)
			if err != nil {
				t.Error(err)
				return
			}
			principal, err := mcpauth.Principal[accountPrincipal](info)
			if err != nil || principal.Account != "account-1" || info.UserID != "binding" || !info.Expiration.Equal(expiration) || info.Scopes[0] != "read" {
				t.Errorf("conversion changed: %+v %v", info, err)
				return
			}
			info.Scopes[0] = "changed"
			for key := range info.Extra {
				delete(info.Extra, key)
			}
		})
	}
	group.Wait()
}

func TestCancellationAndCredentialTranslation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var reported error
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{Validate: func(callbackContext context.Context, credential string, request *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		if callbackContext != ctx || credential != "exact-token" || request.URL.Path != "/mcp" {
			t.Fatal("callback inputs changed")
		}
		return nil, callbackContext.Err()
	}, ReportFailure: func(reportContext context.Context, err error) {
		if reportContext != ctx {
			t.Fatal("report context changed")
		}
		reported = err
	}})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/mcp", nil)
	info, err := verifier.Verify(ctx, "exact-token", request)
	if info != nil || err == nil || err.Error() != "token verification failed" || !errors.Is(reported, context.Canceled) {
		t.Fatal("cancellation contract failed")
	}
	verifier, err = mcpauth.NewVerifier(mcpauth.Config[accountPrincipal]{Validate: func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[accountPrincipal], error) {
		return nil, errors.Join(errors.New("private detail"), mcpauth.ErrUnauthenticated)
	}, ReportFailure: func(context.Context, error) { t.Fatal("authentication reported") }})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := verifier.Verify(ctx, "", nil); info != nil || err != auth.ErrInvalidToken {
		t.Fatal("SDK error identity lost")
	}
}

func TestNilInterfacePrincipal(t *testing.T) {
	token, err := mcpauth.NewVerifiedToken[any]("binding", time.Now().Add(time.Hour), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[any]{Validate: func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[any], error) { return token, nil }, ReportFailure: func(context.Context, error) { t.Fatal("unexpected failure") }})
	if err != nil {
		t.Fatal(err)
	}
	info, err := verifier.Verify(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := mcpauth.Principal[any](info)
	if err != nil || principal != nil {
		t.Fatal("nil interface principal lost")
	}
}

func TestTypedNilInterfacePrincipal(t *testing.T) {
	var typedNil *accountPrincipal
	token, err := mcpauth.NewVerifiedToken[any]("binding", time.Now().Add(time.Hour), nil, typedNil)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := mcpauth.NewVerifier(mcpauth.Config[any]{
		Validate:      func(context.Context, string, *http.Request) (*mcpauth.VerifiedToken[any], error) { return token, nil },
		ReportFailure: func(context.Context, error) { t.Fatal("unexpected failure") },
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := verifier.Verify(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := mcpauth.Principal[any](info)
	if err != nil {
		t.Fatal(err)
	}
	pointer, ok := principal.(*accountPrincipal)
	if !ok || pointer != nil || principal == nil {
		t.Fatal("typed nil principal lost its dynamic type")
	}
}
