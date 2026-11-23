#!/bin/sh
# Fails unless the Go running this job is the one go.mod's toolchain line names.
set -eu
go version
want=$(awk '$1 == "toolchain" { print $2 }' go.mod)
got=$(go env GOVERSION)
if [ -z "$want" ] || [ "$got" != "$want" ]; then
	echo "This job runs $got, but go.mod's toolchain line asks for ${want:-nothing}. Make setup-go and go.mod agree."
	exit 1
fi
