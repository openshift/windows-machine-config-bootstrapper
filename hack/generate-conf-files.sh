#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

GOFLAGS=${1:-}
WMCO_ROOT=$(dirname ${BASH_SOURCE})/..
# Use bindata to access data files in wmcb binary
set -x
go run ${GOFLAGS} github.com/go-bindata/go-bindata/v3/go-bindata \
    -pkg "bootstrapper" \
    -nocompress \
    -nometadata \
    -prefix "${WMCO_ROOT}/pkg/bootstrapper/" \
    -o "${WMCO_ROOT}/pkg/bootstrapper/bindata.go" \
    ${WMCO_ROOT}/pkg/bootstrapper/templates
