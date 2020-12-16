#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Static analysis scripts
cd $(dirname $0)/..

source scripts/ci-lint-install.sh
source scripts/ci-lint-fmt.sh

test -z "$(golint `go list ./...` | tee /dev/stderr)"
go vet -all ./...
staticcheck ./...
gosec -quiet -exclude G104 ./... # Ignoring gosec unhandled errors warning due to many false positives.
misspell cmd/**/*.{go,sh} internal/**/*.{go} README.md
unparam ./...
httperroryzer ./...
