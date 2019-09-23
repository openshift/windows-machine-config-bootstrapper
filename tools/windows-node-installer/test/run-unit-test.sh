#!/bin/bash

WNI_PREFIX="github.com/openshift/windows-machine-config-operator/tools/windows-node-installer"
go test $(go list ${WNI_PREFIX}/pkg...) -v