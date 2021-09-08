all: build build-wmcb-unit-test build-wmcb-e2e-test verify-all

PACKAGE=github.com/openshift/windows-machine-config-bootstrapper
MAIN_PACKAGE=$(PACKAGE)/cmd/bootstrapper

GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on

.PHONY: build
build:
	$(GO_BUILD_ARGS) GOOS=windows go build -o wmcb.exe  $(MAIN_PACKAGE)

.PHONY: build-wmcb-unit-test
build-wmcb-unit-test:
	$(GO_BUILD_ARGS) GOOS=windows GOFLAGS=-v go test -c ./pkg/... -o wmcb_unit_test.exe

.PHONY: build-wmcb-e2e-test
build-wmcb-e2e-test:
	$(GO_BUILD_ARGS) GOOS=windows GOFLAGS=-v go test -c ./test/e2e... -o wmcb_e2e_test.exe

test-e2e-prepared-node:
	$(GO_BUILD_ARGS) GOOS=windows go test -run=TestBootstrapper ./test/e2e

.PHONY: run-wmcb-ci-e2e-test
run-wmcb-ci-e2e-test:
	hack/run-wmcb-ci-e2e-test.sh

.PHONY: verify-all
# TODO: Add other verifications
verify-all:
	hack/verify-gofmt.sh
	hack/verify-vendor.sh
