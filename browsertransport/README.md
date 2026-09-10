# Browser Transport: Render JavaScript Pages

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/browsertransport`

Use `browsertransport` when page content needs JavaScript execution.
A `Session` uses one browser across short-lived render tabs.
Profiles support direct connections, authenticated HTTP proxies, and SOCKS proxies.

## Start with a Local Page

Install Chrome or Chromium before this example. Set `LaunchOptions.ExecPath`
when the browser executable needs an explicit path.
Save this example as `main.go` in your application module. Run `go run main.go`.
The output is `Rendered`.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/tyemirov/utils/browsertransport"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		fmt.Fprint(response, `<html><head><title>Initial</title></head><body><script>document.title="Rendered";</script><p id="ready">Ready</p></body></html>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	profile, err := browsertransport.InferBrowserProfile("", false)
	if err != nil {
		log.Fatal(err)
	}
	session, err := browsertransport.NewSession(ctx, profile, browsertransport.LaunchOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()
	result, err := session.RenderPage(ctx, browsertransport.PageRequest{
		TargetURL:    server.URL,
		WaitSelector: "#ready",
		Timeout:      10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Title)
}
```

## Select an API

| Task | Entry point |
| --- | --- |
| Interpret a direct or proxy URL | `InferBrowserProfile` |
| Share one browser across requests | `NewSession`, `Session.RenderPage` |
| Run custom chromedp actions in a tab | `Session.WithTab` |
| Render one page with an automatic session lifetime | `RenderPage` |
| Render a group of pages | `RenderPages` |

The application controls browser installation, the session lifetime, and the
selector that indicates page readiness. See [source contracts](browsertransport.go)
for launch options and result fields.
For HTTP requests without browser execution, use [httptransport](../httptransport/README.md).
The [crawler package](../crawler/README.md) imports this package for its browser transport API.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./browsertransport
```

The [render tests](render_test.go) and [session tests](session_test.go)
use the browser test infrastructure in this package.
