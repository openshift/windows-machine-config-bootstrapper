#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

RELEASE="release-4.6"
GIT_CLONE="git clone --branch $RELEASE --single-branch"
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

# Create a temporary directory for the repos
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

pushd "$TMP_DIR"
# Build hybrid-overlay-node.exe
$GIT_CLONE https://github.com/openshift/ovn-kubernetes.git
cd ovn-kubernetes/go-controller/
make windows
HYBRID_OVERLAY_BIN=$TMP_DIR"/ovn-kubernetes/go-controller/_output/go/bin/windows/hybrid-overlay-node.exe"

# Build kubelet.exe
cd "$TMP_DIR"
$GIT_CLONE https://github.com/openshift/kubernetes.git
cd kubernetes
KUBE_BUILD_PLATFORMS=windows/amd64 make WHAT=cmd/kubelet
KUBELET_BIN=$TMP_DIR"/kubernetes/_output/local/bin/windows/amd64/kubelet.exe"
popd

# Build the unit test binary
cd "${WMCO_ROOT}"
make build-wmcb-unit-test
make build-wmcb-e2e-test


cd "${WMCB_TEST_DIR}"
# Transfer the files and run the unit and e2e tests
CGO_ENABLED=0 GO111MODULE=on go test -v -run=TestWMCB \
-filesToBeTransferred="../../../wmcb_unit_test.exe,../../../wmcb_e2e_test.exe,$HYBRID_OVERLAY_BIN,$KUBELET_BIN" \
-vmCreds="$VM_CREDS" $SKIP_VM_SETUP -timeout=30m .
