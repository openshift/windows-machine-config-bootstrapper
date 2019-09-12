all: build

PACKAGE=github.com/openshift/windows-machine-config-operator
MAIN_PACKAGE=$(PACKAGE)/cmd/bootstrapper

GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on

# TODO: Pin go versions

.PHONY: build
build:
	$(GO_BUILD_ARGS) go build -o wmcb  $(MAIN_PACKAGE)
