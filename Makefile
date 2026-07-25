GO ?= go

.PHONY: fmt vet test race integration check build

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

integration:
	$(GO) test -tags=integration ./... -count=1 -timeout=10m

check: fmt vet test race

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/cutline ./cmd/cutline
