.PHONY: build test race vet complexity fmt-check check integration ui-check ui-build api-generate api-check

# npm packages can contain Go examples; do not execute third-party test fixtures.
GO_PACKAGES := ./api/... ./cmd/... ./internal/...

build: api-check ui-build
	go build -o bin/server ./cmd/server
	go build -o bin/developa ./cmd/developa

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

check: fmt-check vet complexity test api-check ui-check

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

integration:
	@test -n "$$DEVELOPA_TEST_DATABASE_URL" || (echo "Set DEVELOPA_TEST_DATABASE_URL to an isolated test database"; exit 1)
	go test -count=1 ./internal/store/postgres ./internal/transport/http
