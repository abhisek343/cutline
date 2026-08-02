GO ?= go

.PHONY: fmt fmt-check vet test race integration temporal-integration check build benchmark release package

fmt:
	$(GO) fmt ./...

fmt-check:
	@unformatted="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$unformatted" ]; then \
		printf 'gofmt required for:\n%s\n' "$$unformatted" >&2; \
		exit 1; \
	fi

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

check: fmt-check vet test race

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/cutline ./cmd/cutline

benchmark:
	$(GO) test ./test/release -run TestReferenceCheckoutReleaseGate -count=1 -timeout=10m

release: check integration temporal-integration benchmark build

package:
	@test -n "$(VERSION)" || (echo "VERSION is required (for example VERSION=v0.1.0)" >&2; exit 2)
	VERSION="$(VERSION)" OUT_DIR="$(OUT_DIR)" bash scripts/package-release.sh "$(VERSION)"
