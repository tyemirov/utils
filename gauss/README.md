# GAuss: Google OAuth Authorization

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/gauss`

Use `gauss` for Google consent URLs, authorization-code exchange, token refresh,
and authenticated HTTP requests. Typed scopes cover Gmail, YouTube, and identity access.

## Start without a Provider Request

Save this example as `main.go` in your application module. Run `go run main.go`.
It constructs a consent URL from example values and prints `true`.

```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/tyemirov/utils/gauss"
)

func main() {
	client, err := gauss.New(gauss.Config{
		ClientID:     "example-client-id",
		ClientSecret: "example-client-secret",
		RedirectURL:  "http://localhost:8080/oauth/callback",
		Scopes:       []gauss.Scope{gauss.ScopeGmailReadonly},
	})
	if err != nil {
		log.Fatal(err)
	}
	consentURL := client.AuthURL("example-state")
	fmt.Println(strings.Contains(consentURL, "access_type=offline"))
}
```

## Connect an Application

1. Supply registered Google credentials and the application callback URL to `New`.
2. Generate and store a random state value for each authorization request.
3. Redirect the user to the URL from `AuthURL`.
4. Verify the callback state against the stored value.
5. Exchange the returned code with `Exchange`.
6. Store the token through the application token repository.
7. Use `HTTPClient` for authenticated requests or `FetchUserInfo` for profile information.

The application controls callback routes, state verification, token storage, and
scope selection. Offline access is enabled by default. `AuthURL` requests
consent in that mode. Google controls refresh-token issuance.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./gauss
```

See [client contracts](gauss.go), [scope constants](scopes.go), and
[local endpoint tests](gauss_test.go). These tests show the package protocol
contract. Google account access needs separate qualification.
