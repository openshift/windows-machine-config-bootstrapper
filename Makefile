all: build build-tools

PACKAGE=github.com/openshift/windows-machine-config-operator
MAIN_PACKAGE=$(PACKAGE)/cmd/bootstrapper
TOOLS_DIR=./tools/windows-node-installer

GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on

# TODO (suhanime): Export GOPATH if not set
# TODO (suhanime): Pin go versions and lint
# TODO (suhanime): Enable linting when we don't have unimplemented methods

.PHONY: build
build:
	$(GO_BUILD_ARGS) go build -o wmcb  $(MAIN_PACKAGE)

.PHONY: build-tools
build-tools:
	$(GO_BUILD_ARGS) go build -o wni $(TOOLS_DIR)/main.go
