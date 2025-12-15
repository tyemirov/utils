# `github.com/tyemirov/utils/llm`

## Overview

`llm` provides the reusable plumbing for chat-completion based flows. It exposes:

- `Message`, `ChatRequest`, and the `ChatClient` interface so call sites can build prompts without depending on HTTP details.
- `Client`, a minimal HTTP adapter to `/chat/completions` with request validation, timeout handling, and error trimming.
- `Factory`, a wrapper that layers configurable retry/backoff semantics and still satisfies the `ChatClient` interface.
- Helper types (`Config`, `ResponseFormat`, `RetryPolicy`, etc.) so packages can describe their needs declaratively.

The package is dependency-free beyond the Go standard library.

## Usage

Construct a client (or factory) with `llm.Config`:

```go
llmConfig := llm.Config{
	BaseURL:             os.Getenv("OPENAI_BASE_URL"),
	APIKey:              os.Getenv("OPENAI_API_KEY"),
	Model:               "gpt-4.1-mini",
	MaxCompletionTokens: 512,
	Temperature:         0.2,
	HTTPClient:          &http.Client{Timeout: 30 * time.Second},
	RequestTimeout:      60 * time.Second,
	RetryAttempts:       3,
	RetryInitialBackoff: 200 * time.Millisecond,
	RetryMaxBackoff:     2 * time.Second,
	RetryBackoffFactor:  2,
}
```

- Use `client, _ := llm.NewClient(cfg)` when you want a single request/response without automatic retries.
- Use `factory, _ := llm.NewFactory(cfg)` when you want retry/backoff semantics. The factory still implements `ChatClient`.

