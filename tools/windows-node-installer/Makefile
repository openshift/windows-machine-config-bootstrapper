# Using Go Module
export GO111MODULE=on

# Check go fmt with lint
GOLANGCI_VERSION := v1.15.0
HAS_GOLANGCI := $(shell which golangci-lint)

lint:
ifndef HAS_GOLANGCI
	curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(GOPATH)/bin
	${GOLANGCI_VERSION}
endif
	golangci-lint run

# Build tool binary
build:
	go build .
