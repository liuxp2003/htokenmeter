# htokenmeter dev tasks
GO ?= go
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/lxp/htokenmeter/common.Version=$(VERSION)

.PHONY: build test web dev clean dist cross-arm64 cross-amd64

build: web ## build the gateway binary (stripped)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o htokenmeter .

test: ## run all tests
	$(GO) test ./... -count=1

web: ## build the console into web/dist
	cd web && (npm ci || npm install) && npm run build

dev: ## run locally on :8317 with a dev db
	$(GO) run . --port 8317 --config-dir /tmp/htm-dev

cross-arm64: web
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/htokenmeter-linux-arm64 .

cross-amd64: web
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/htokenmeter-linux-amd64 .

dist: cross-arm64 cross-amd64 ## cross-compile both target architectures
	@ls -la dist/

clean:
	rm -rf dist htokenmeter
