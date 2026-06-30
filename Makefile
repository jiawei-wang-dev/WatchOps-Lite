.PHONY: run test lint fmt verify

CONFIG ?= configs/config.json

run:
	go run ./cmd/server -config $(CONFIG)

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

verify:
	./scripts/verify.sh
