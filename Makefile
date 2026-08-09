.PHONY: build test vet fmt check migrate seed-demo contract-check web-install web-check

build:
	go build ./cmd/cookies-api
	go build ./cmd/cookies-seed

migrate:
	go run ./cmd/cookies-migrate

seed-demo:
	go run ./cmd/cookies-seed

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(go list -f '{{.Dir}}' ./... | xargs -n1 -I{} find {} -name '*.go' -type f)

check:
	@files="$$(gofmt -l $$(go list -f '{{.Dir}}' ./... | xargs -n1 -I{} find {} -name '*.go' -type f))"; test -z "$$files" || (echo "Unformatted Go files:"; echo "$$files"; exit 1)
	go vet ./...
	go test ./...
	$(MAKE) web-check
	$(MAKE) contract-check

contract-check:
	npm run contract:check

web-install:
	npm ci

web-check:
	npm run test
	npm run build
