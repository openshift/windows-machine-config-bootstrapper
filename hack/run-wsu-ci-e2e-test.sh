#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

WMCO_ROOT=$(pwd)
TEST_DIR=$WMCO_ROOT/internal/test/wsu

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

# Set up hybrid networking on the cluster, a requirement of OVNKubernetes on Windows
oc patch network.operator cluster --type=merge -p '{"spec":{"defaultNetwork":{"ovnKubernetesConfig":{"hybridOverlayConfig":{"hybridClusterNetwork":[{"cidr":"10.132.0.0/14","hostPrefix":23}]}}}}}'

# Even though the above patch will take non-zero time to apply, no delay is needed, as the WSU test suite
# begins by creating a VM, an action which will take 4+ minutes. More than enough time for the patch to apply.

# Run the test suite
cd $TEST_DIR
GO_BUILD_ARGS=CGO_ENABLED=0 GO111MODULE=on CLUSTER_ADDR=$CLUSTER_ADDR WSU_PATH=$WMCO_ROOT/tools/ansible/tasks/wsu/main.yaml go test -v -timeout 60m .

exit 0
