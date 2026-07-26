GO ?= go

.PHONY: fmt vet test race integration temporal-integration check build benchmark release

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

temporal-integration:
	bash test/temporal/run-integration.sh

check: fmt vet test race

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/cutline ./cmd/cutline


benchmark:
	$(GO) test ./test/release -run TestReferenceCheckoutReleaseGate -count=1 -timeout=10m

release: check benchmark build
