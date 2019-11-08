#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# TODO: Add steps to execute code that will kick off the e2e tests in CI
WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
INTERNAL_TEST_DIR=$WMCO_ROOT/internal/test/

# Build the unit test binary
cd "${WMCO_ROOT}"
make build-wmcb-unit-test

# Transfer the binary and run the unit tests
cd "${INTERNAL_TEST_DIR}"
CGO_ENABLED=0 GO111MODULE=on go test -c wmcb_test.go -o wmcb_framework
./wmcb_framework -binaryToBeTransferred=../../wmcb_unit_test.exe

