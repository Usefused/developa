.PHONY: build test race vet complexity fmt-check check integration ui-check ui-build installer-check api-generate api-check release-check release-snapshot

# npm packages can contain Go examples; do not execute third-party test fixtures.
GO_PACKAGES := ./api/... ./cmd/... ./internal/...
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

build: api-check ui-build
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/denverr ./cmd/denverr

test:
	go test $(GO_PACKAGES)

race:
	go test -race $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

complexity:
	go tool gocyclo -over 10 ./api ./cmd ./internal

fmt-check:
	@test -z "$$(gofmt -l ./api ./cmd ./internal)" || (gofmt -l ./api ./cmd ./internal; exit 1)

check: fmt-check vet complexity test api-check ui-check installer-check

api-generate:
	go run ./cmd/openapi

api-check:
	go run ./cmd/openapi -check
	npm run api:validate

ui-check:
	npm run lint
	npm test
	npm run build:check

ui-build:
	npm run build

installer-check:
	sh scripts/install_test.sh

integration:
	@test -n "$$DENVERR_TEST_DATABASE_URL" || (echo "Set DENVERR_TEST_DATABASE_URL to an isolated test database"; exit 1)
	go test -count=1 ./internal/store/postgres ./internal/transport/http

release-check:
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean
