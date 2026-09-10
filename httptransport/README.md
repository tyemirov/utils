# HTTP Transport: Explicit Proxy Connections

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/httptransport`

Use `httptransport` to construct HTTP clients with direct, HTTP proxy, or SOCKS
connections. Direct profiles bypass `HTTP_PROXY` and `HTTPS_PROXY` from the
process environment. Proxy selection comes from the supplied profile.

## Start

Save this example as `main.go` in your application module. Run `go run main.go`.
It starts a local server and prints `200`.

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/tyemirov/utils/httptransport"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	profile, err := httptransport.InferProfile("", false)
	if err != nil {
		log.Fatal(err)
	}
	client, err := httptransport.NewClient(profile, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(response.StatusCode)
	if err := response.Body.Close(); err != nil {
		log.Fatal(err)
	}
}
```

To use a proxy, supply its URL to `InferProfile`.
The application controls request content, response processing, and client cleanup.
[Browser transport](../browsertransport/README.md) and [crawler](../crawler/README.md)
use this implementation.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./httptransport
```

See [source contracts](httptransport.go) and [protocol tests](httptransport_test.go)
for proxy behavior and timeout settings.
