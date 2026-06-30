.PHONY: run test lint fmt

run:
	go run ./cmd/server

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
