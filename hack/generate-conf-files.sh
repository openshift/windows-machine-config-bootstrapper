#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

WMCO_ROOT=$(dirname ${BASH_SOURCE})/..
# Use bindata to access data files in wmcb binary
set -x
go run github.com/go-bindata/go-bindata/go-bindata\
    -pkg "bootstrapper" \
    -nocompress \
    -nometadata \
    -prefix "${WMCO_ROOT}/pkg/bootstrapper/" \
    -o "${WMCO_ROOT}/pkg/bootstrapper/bindata.go" \
    ${WMCO_ROOT}/pkg/bootstrapper/templates
