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
# Exclude rule G114 due to example/server using it as a demo.
gosec -quiet -exclude G114 ./...
