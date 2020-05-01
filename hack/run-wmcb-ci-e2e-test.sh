#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

SKIP_VM_SETUP=""
VM_CREDS=""

while getopts ":v:s" opt; do
  case ${opt} in
    v ) # process option for providing existing VM credentials
      VM_CREDS=$OPTARG
      ;;
    s ) # process option for skipping setup in VMs
      SKIP_VM_SETUP="-skipVMSetup"
      ;;
    \? )
      echo "Usage: $0 [-v] [-s]"
      exit 0
      ;;
  esac
done

# TODO: Add steps to execute code that will kick off the e2e tests in CI
WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
WMCB_TEST_DIR=$WMCO_ROOT/internal/test/wmcb

# Build the unit test binary
cd "${WMCO_ROOT}"
make build-wmcb-unit-test
make build-wmcb-e2e-test


cd "${WMCB_TEST_DIR}"
# Transfer the files and run the unit and e2e tests
CGO_ENABLED=0 GO111MODULE=on go test -v -run=TestWMCB -filesToBeTransferred="../../../wmcb_unit_test.exe,../../../wmcb_e2e_test.exe,powershell/wget-ignore-cert.ps1" -vmCreds="$VM_CREDS" $SKIP_VM_SETUP -timeout=30m .
