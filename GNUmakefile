SHELL := /bin/sh

GOLANGCI_LINT_VERSION := v2.13.2

.PHONY: build check docs docs-check examples fmt lint test tools vet

build:
	go build ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

test:
	go test ./...

docs:
	go tool tfplugindocs generate --provider-name foundry

docs-check: docs
	git diff --exit-code -- docs

examples:
	./scripts/validate-example.sh terraform
	@if command -v tofu >/dev/null 2>&1; then ./scripts/validate-example.sh tofu; fi

tools:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) version
	go tool tfplugindocs --version

check: fmt vet lint test build docs-check examples
