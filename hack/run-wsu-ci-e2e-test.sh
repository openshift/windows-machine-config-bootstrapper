#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# TODO: Add input validation
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

WMCO_ROOT=$(pwd)
TEST_DIR=$WMCO_ROOT/internal/test/wsu

# Make gopath if doesnt exist
mkdir -p $GOPATH

# The WSU playbook requires the cluster address, we parse that here using oc
CLUSTER_ADDR=$(oc cluster-info | head -n1 | sed 's/.*\/\/api.//g'| sed 's/:.*//g')

# Even though the above patch will take non-zero time to apply, no delay is needed, as the WSU test suite
# begins by creating a VM, an action which will take 4+ minutes. More than enough time for the patch to apply.

# Run the test suite
cd $TEST_DIR
GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on CLUSTER_ADDR=$CLUSTER_ADDR WSU_PATH=$WMCO_ROOT/tools/ansible/tasks/wsu/main.yaml go test -v -vmCreds="$VM_CREDS" $SKIP_VM_SETUP -timeout 60m .

exit 0
