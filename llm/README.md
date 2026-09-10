# LLM Client: Chat Requests and Retries

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/llm`

Use `llm` to send JSON chat-completion requests through a configured HTTP
endpoint. `Client` sends requests to `/chat/completions`.
`Factory` adds configurable retries and implements the same `ChatClient` interface.
The package imports only the Go standard library.

## Start with a Local Endpoint

Save this example as `main.go` in your application module. Run `go run main.go`.
It starts a local protocol server and prints `Hello`.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/tyemirov/utils/llm"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client, err := llm.NewClient(llm.Config{
		BaseURL:        server.URL + "/v1",
		APIKey:         "local-example",
		Model:          "local-example-model",
		HTTPClient:     server.Client(),
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	reply, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Say hello"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply)
}
```

## Select an API

| Task | Entry point |
| --- | --- |
| Send a request | `NewClient`, `Client.Chat` |
| Add retries and delay limits | `NewFactory`, `RetryPolicy` |
| Supply a client to application code | `ChatClient` |
| Request a structured response | `ChatRequest.ResponseFormat` |
| Control retry timing in tests | `WithSleepFunc` |

The application controls endpoint and model selection, credentials, prompts, and
response use. The local example verifies this package protocol.
Each external provider needs qualification against the request and response contracts.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./llm
```

See [client contracts](client.go), [retry implementation](factory.go), and
[request tests](client_test.go).
