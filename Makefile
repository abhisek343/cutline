GO ?= go

.PHONY: fmt vet test race check build

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

check: fmt vet test race

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/cutline ./cmd/cutline
