.PHONY: run test lint fmt verify eval-retrieval benchmark-agent check-deps e2e-demo

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

eval-retrieval:
	./scripts/eval_retrieval.sh

benchmark-agent:
	./scripts/benchmark_agent.sh

check-deps:
	./scripts/check_dependencies.sh

e2e-demo:
	./scripts/e2e_demo_check.sh
