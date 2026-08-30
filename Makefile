.PHONY: all fmt fmt-check vet test race build build-linux security ci

GO ?= go
BINARY := bin/github-radar

all: ci

fmt:
	$(GO) fmt ./...

fmt-check:
	@files="$$(gofmt -l .)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/github-radar

build-linux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(BINARY)-linux-amd64 ./cmd/github-radar

security:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

ci: fmt-check vet test race build-linux
