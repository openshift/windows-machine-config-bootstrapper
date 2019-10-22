all: build build-tools build-wmcb-unit-test verify-all

PACKAGE=github.com/openshift/windows-machine-config-operator
MAIN_PACKAGE=$(PACKAGE)/cmd/bootstrapper
TOOLS_DIR=$(PACKAGE)/tools/windows-node-installer

GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on

# TODO (suhanime): Export GOPATH if not set
# TODO (suhanime): Pin go versions and lint
# TODO (suhanime): Enable linting when we don't have unimplemented methods

.PHONY: build
build:
	$(GO_BUILD_ARGS) GOOS=windows go build -o wmcb.exe  $(MAIN_PACKAGE)

.PHONY: build-wmcb-unit-test
build-wmcb-unit-test:
	GOOS=windows GOFLAGS=-v go test -c ./pkg/... -o wmcb_unit_test.exe

test-e2e-prepared-node:
	GOOS=windows go test -run=TestBootstrapper ./test/e2e

.PHONY: build-tools
build-tools:
	$(GO_BUILD_ARGS) go build -o wni $(TOOLS_DIR)

.PHONY: test-e2e-tools
test-e2e-tools:
	$(GO_BUILD_ARGS) go test $(TOOLS_DIR)/test/e2e/... -timeout 20m -v

.PHONY: run-wmcb-ci-e2e-test
run-wmcb-ci-e2e-test:
	hack/run-wmcb-ci-e2e-test.sh

.PHONY: run-wsu-ci-e2e-test
run-wsu-ci-e2e-test:
	hack/run-wsu-ci-e2e-test.sh

.PHONY: verify-all
# TODO: Add other verifications
verify-all:
	hack/verify-gofmt.sh
