.PHONY: all fmt fmt-check vet test test-js race build build-linux security ci

GO ?= go
NODE ?= node
BINARY := bin/repotempo

all: ci

fmt:
	$(GO) fmt ./...

fmt-check:
	@files="$$(gofmt -l .)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-js:
	$(NODE) --test scripts/*.test.mjs

race:
	$(GO) test -race ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/repotempo
	cp $(BINARY) bin/github-radar

build-linux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(BINARY)-linux-amd64 ./cmd/repotempo
	cp $(BINARY)-linux-amd64 bin/github-radar-linux-amd64

security:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

ci: fmt-check vet test test-js race build-linux
