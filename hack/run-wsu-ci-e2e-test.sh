#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

WMCO_ROOT=$(pwd)
TEST_DIR=$WMCO_ROOT/tools/ansible/tasks/wsu/test/e2e

# Make gopath if doesnt exist
mkdir -p $GOPATH

# If current user cannot be found add it to /etc/passwd. This is the case when this is run by an OpenShift cluster,
# as OpenShift uses an arbitrarily assigned user ID to run the container.
if ! whoami; then
  echo "Creating user"
  echo "tempuser:x:$(id -u):$(id -g):,,,:${HOME}:/bin/bash" >> /etc/passwd
  echo "tempuser:x:$(id -G | cut -d' ' -f 2)" >> /etc/group
fi

# The WSU playbook requires the cluster address, we parse that here using oc
CLUSTER_ADDR=$(oc cluster-info | head -n1 | sed 's/.*\/\/api.//g'| sed 's/:.*//g')

# Run the test suite
cd $TEST_DIR
GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on CLUSTER_ADDR=$CLUSTER_ADDR WSU_PATH=$WMCO_ROOT/tools/ansible/tasks/wsu/main.yaml go test -v .

exit 0
