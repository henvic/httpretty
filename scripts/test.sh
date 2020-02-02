#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Static analysis scripts
cd $(dirname $0)/..

echo "Linting code."
test -z "$(golint `go list ./...` | tee /dev/stderr)"

echo "Examining source code against code defect."
go vet $(go list ./...)
go vet -vettool=$(which shadow)

echo "Running staticcheck toolset."
staticcheck ./...

echo "Checking if code contains security issues."
gosec -quiet ./...

echo "Running tests with data race detector"
go test ./... -race
