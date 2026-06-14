.PHONY: build test lint vet clean docker cross-compile bench fuzz e2e

BINARY    := netdata-postgres-mcp
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
GO        := go

## build: Build for the current platform
build:
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/netdata-postgres-mcp

## test: Run all tests with race detector
test:
	$(GO) test -v -race -count=1 ./...

## vet: Run go vet
vet:
	$(GO) vet ./...

## lint: Run staticcheck (install if missing)
lint: vet
	@which staticcheck > /dev/null 2>&1 || $(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

## clean: Remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -f dist/*

## docker: Build Docker image
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .
	docker tag $(BINARY):$(VERSION) $(BINARY):latest

## cross-compile: Build for Linux (amd64, arm64) and Windows (amd64)
cross-compile:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64      ./cmd/netdata-postgres-mcp
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64      ./cmd/netdata-postgres-mcp
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe ./cmd/netdata-postgres-mcp
	@echo "Built binaries in dist/"

## migrate: Run database migrations
migrate: build
	./$(BINARY) migrate

## run: Start the sidecar service
run: build
	./$(BINARY) run

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'

## bench: Run benchmarks
bench:
	$(GO) test -bench=. -benchmem -count=3 ./...

## fuzz: Run fuzz tests (30s each)
fuzz:
	$(GO) test -fuzz=FuzzParseTimeArg -fuzztime=30s ./internal/mcp/
	$(GO) test -fuzz=FuzzRound2 -fuzztime=30s ./internal/mcp/
	$(GO) test -fuzz=FuzzDetectBottlenecks -fuzztime=30s ./internal/mcp/
	$(GO) test -fuzz=FuzzBuildSummary -fuzztime=30s ./internal/mcp/

## e2e: Run end-to-end tests via Docker Compose
e2e:
	./scripts/e2e-test.sh
