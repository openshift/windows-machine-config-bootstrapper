#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# downloads the oc binary. We can use oc directly from /tmp
get_oc(){
  # Download the oc binary only if it is not already available
  # We do not validate the version of oc if it is available already
  if type oc >/dev/null 2>&1; then
    which oc
    return
  fi

  OC_DIR=/tmp/oc
  curl -L -s https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.5.8/openshift-client-linux-4.5.8.tar.gz -o openshift-origin-client-tools.tar.gz \
    && tar -xzf openshift-origin-client-tools.tar.gz \
    && mv oc $OC_DIR \
    && rm -rf ./openshift*

  echo $OC_DIR
}

# delete_job deletes the deployed test pod
delete_job(){
  if ! $1 delete -f internal/test/wmcb/deploy/job.yaml -n default; then
    echo "no job found"
  fi
}

WMCO_ROOT=$(dirname "${BASH_SOURCE}")/..
cd "${WMCO_ROOT}"

# If ARTIFACT_DIR is not set, create a temp directory for artifacts
ARTIFACT_DIR=${ARTIFACT_DIR:-}
if [ -z "$ARTIFACT_DIR" ]; then
  ARTIFACT_DIR=`mktemp -d`
  echo "ARTIFACT_DIR is not set. Artifacts will be stored in: $ARTIFACT_DIR"
  export ARTIFACT_DIR=$ARTIFACT_DIR
fi

OC=$(get_oc)

if ! $OC create secret generic cloud-private-key --from-file=private-key.pem=$KUBE_SSH_KEY_PATH -n default; then
    echo "cloud-private-key already present"
fi
if ! $OC create secret generic aws-creds --from-file=credentials=$AWS_SHARED_CREDENTIALS_FILE -n default; then
    echo "aws credentials already present"
fi
if ! $OC apply -f internal/test/wmcb/deploy/role.yaml -n default; then
    echo "role already present"
fi
if ! $OC apply -f internal/test/wmcb/deploy/rolebinding.yaml -n default; then
    echo "rolebinding already present"
fi

sed -i "s~ARTIFACT_DIR_VALUE~${ARTIFACT_DIR}~g" internal/test/wmcb/deploy/job.yaml
sed -i "s~REPLACE_IMAGE~${WMCB_IMAGE}~g" internal/test/wmcb/deploy/job.yaml

# declare required labels
declare -a NS_LABELS=(
  # turn on the automatic label synchronization required for PodSecurity admission
  "security.openshift.io/scc.podSecurityLabelSync=true"
  # set pods security profile to privileged. See https://kubernetes.io/docs/concepts/security/pod-security-admission/#pod-security-levels
  "pod-security.kubernetes.io/enforce=privileged"
)

# apply required labels
if ! $OC label ns default "${NS_LABELS[@]}" --overwrite; then
  error-exit "error setting labels ${NS_LABELS[@]} in namespace default"
fi

# deploy the test pod on test cluster
if ! $OC apply -f internal/test/wmcb/deploy/job.yaml -n default; then
    echo "job already deployed"
fi

# stopping condition for the while loop. Terminated the loop if the pod fails to come up within 2 minutes.
end=$((SECONDS+120))

# wait while the pod enters running state
while [ "Running" != "$("${OC}" get pods -n default | grep -i wmcb-e2e-test* | awk '{print $3}')" ]
do
  if [ $SECONDS -gt $end ]; then
    echo "unable to determine the pod status for 2 minutes. Terminating..."
    delete_job $OC
    exit 1
  fi
  sleep 5s
done

echo "Starting Log"

# streams the log from test pod running on the test cluster
podName=$("${OC}" get pods -n default | grep -i wmcb-e2e-test* | awk '{print $1}')
if ! $OC logs -f ${podName} -n default ; then
  echo "error getting logs for container"
fi

podStatus=$("${OC}" get pod "${podName}" -n default | grep -w "${podName}" | awk '{print $3}')
# if the pod status is not `Completed` this means that the tests have failed , error out
if [ "Completed" != "${podStatus}" ] ; then
  echo "Pod failed with status ${podStatus}"
  delete_job $OC
  echo "tests failed"
  exit 1
fi

delete_job $OC
echo "test completed successfully"
exit 0
