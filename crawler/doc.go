// Package crawler provides a reusable crawling service that fetches web pages,
// applies configurable rules, and emits normalized results. It supports proxy
// rotation, retry with backoff, rate limiting, platform-specific hooks, and
// extensible response handling through the ResponseHandler interface.
package crawler
