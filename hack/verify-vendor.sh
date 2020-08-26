#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

WMCB_ROOT=$(pwd)

# Create a temporary location to put our new vendor tree
mkdir -p "${WMCB_ROOT}/_tmp"
_tmpdir="$(mktemp -d "${WMCB_ROOT}/_tmp/wmcb-vendor.XXXXXX")"

# Copy the contents of the WMCB directory into the temporary location
_wmcbtmp="${_tmpdir}/WMCB"
mkdir -p "${_wmcbtmp}"
tar --exclude=.git --exclude="./_*" -c . | (cd "${_wmcbtmp}" && tar xf -)
# Clean up the temp directory
trap "rm -rf ${WMCB_ROOT}/_tmp" EXIT

export GO111MODULE=on

pushd "${_wmcbtmp}" > /dev/null 2>&1
# Destroy deps in the copy of the WMCB tree
rm -rf ./vendor

# Recreate the vendor tree using the clean set we just downloaded
go mod vendor
popd > /dev/null 2>&1

ret=0

pushd "${WMCB_ROOT}" > /dev/null 2>&1
if ! _out="$(diff -Naupr -x "BUILD" -x "AUTHORS*" -x "CONTRIBUTORS*" vendor "${_wmcbtmp}/vendor")"; then
   echo "Your Vendored results are different:" >&2
   echo "${_out}" >&2
   echo "Vendor verify failed." >&2
   echo "${_out}" > vendordiff.patch
   echo "If you're seeing this, run the below command to fix your directories:" >&2
   echo "go mod vendor" >&2
   ret=1
fi
popd > /dev/null 2>&1


exit ${ret}
