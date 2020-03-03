#!/bin/bash
set -euox pipefail

# TODO(henvic): install specific versions of the commands
# when the 3 latest releases of the Go toolchains supports it using @tag.
go install github.com/mattn/goveralls
go install golang.org/x/lint/golint
go install honnef.co/go/tools/cmd/staticcheck
go install github.com/securego/gosec/cmd/gosec
go install golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow
