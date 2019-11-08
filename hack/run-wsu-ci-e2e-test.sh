#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
TEST_DIR=$WMCO_ROOT/tools/ansible/tasks/wsu/test/e2e

sleep 1000
# Required packages to run the test suite
pip2 install --user ansible pywinrm

# The WSU playbook requires the cluster address, we parse that here using oc
CLUSTER_ADDR=$(oc cluster-info | head -n1 | sed 's/.*\/\/api.//g'| sed 's/:.*//g')

# Run the test suite

cd $TEST_DIR
CLUSTER_ADDR=$CLUSTER_ADDR WSU_PATH=$WMCO_ROOT/tools/ansible/tasks/wsu/main.yaml go test -v .

exit 0
