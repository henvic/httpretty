#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Static analysis scripts
cd $(dirname $0)/..

source scripts/ci-lint-install.sh
source scripts/ci-lint-fmt.sh

set -x
go vet ./...
staticcheck ./...
gosec -quiet ./...
