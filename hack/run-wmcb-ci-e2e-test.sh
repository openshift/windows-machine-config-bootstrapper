#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# TODO: Add steps to execute code that will kick off the e2e tests in CI
WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
WMCB_TEST_DIR=$WMCO_ROOT/internal/test/wmcb

# Build the unit test binary
cd "${WMCO_ROOT}"
make build-wmcb-unit-test

# Transfer the binary and run the unit tests
cd "${WMCB_TEST_DIR}"
CGO_ENABLED=0 GO111MODULE=on go test -v -run=TestWMCBUnit -binaryToBeTransferred=../../../wmcb_unit_test.exe -timeout=30m .

