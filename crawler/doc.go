// Package crawler provides a generic, configurable web crawling engine built
// on Colly. It supports concurrent page fetching, retries with exponential
// backoff, rate limiting, proxy rotation with circuit-breaker health tracking,
// and pluggable response processing via the ResponseHandler interface.
//
// Simple consumers use an Evaluator with the default response handler.
// Advanced consumers inject a custom ResponseHandler for full control over
// how fetched documents are processed and results are emitted.
package crawler
