GO ?= go
GOFMT ?= gofmt
STATICCHECK ?= staticcheck
INEFFASSIGN ?= ineffassign

GO_SOURCES := $(shell find . -name '*.go' -not -path './vendor/*' -not -path './.claude/*')
PACKAGES := $(shell go list ./...)
UNIT_PACKAGES := $(shell go list ./... | grep -v '/test$$')
INTEGRATION_PACKAGE := ./test
COVERAGE_THRESHOLD ?= 100
COVERAGE_DIR := .coverage

.PHONY: format check-format lint test test-unit test-integration test-coverage clean ci

format:
	$(GOFMT) -w $(GO_SOURCES)

check-format:
	@formatted="$$($(GOFMT) -l $(GO_SOURCES))"; \
	if [ -n "$$formatted" ]; then \
		echo 'Go files require formatting:'; \
		echo "$$formatted"; \
		exit 1; \
	fi

lint:
	@command -v $(STATICCHECK) >/dev/null 2>&1 || { echo 'staticcheck is required (install via `go install honnef.co/go/tools/cmd/staticcheck@latest`)'; exit 1; }
	@command -v $(INEFFASSIGN) >/dev/null 2>&1 || { echo 'ineffassign is required (install via `go install github.com/gordonklaus/ineffassign@latest`)'; exit 1; }
	$(GO) vet ./...
	$(STATICCHECK) ./...
	$(INEFFASSIGN) ./...

test-unit:
	$(GO) test $(UNIT_PACKAGES)

test-integration:
	$(GO) test $(INTEGRATION_PACKAGE)

test: test-unit test-integration

test-coverage:
	@mkdir -p $(COVERAGE_DIR)
	@fail=0; \
	for pkg in $(UNIT_PACKAGES); do \
		name=$$(echo $$pkg | sed 's|.*/||'); \
		$(GO) test -coverprofile=$(COVERAGE_DIR)/$$name.out $$pkg > /dev/null 2>&1; \
		pct=$$($(GO) tool cover -func=$(COVERAGE_DIR)/$$name.out 2>/dev/null | awk '/^total:/ {gsub(/%/, "", $$NF); print $$NF}'); \
		if [ -z "$$pct" ]; then pct="0.0"; fi; \
		int_pct=$$(echo "$$pct" | awk '{printf "%d", $$1}'); \
		if [ "$$int_pct" -lt $(COVERAGE_THRESHOLD) ]; then \
			echo "FAIL: $$pkg coverage $${pct}% < $(COVERAGE_THRESHOLD)%"; \
			fail=1; \
		else \
			echo "OK:   $$pkg coverage $${pct}%"; \
		fi; \
	done; \
	if [ "$$fail" -eq 1 ]; then exit 1; fi

clean:
	$(GO) clean -cache -testcache
	rm -rf $(COVERAGE_DIR)

ci: check-format lint test test-coverage
